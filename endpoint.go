package renom

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/stun/v3"
	"github.com/pion/webrtc/v4"
)

type manualEndpoint struct {
	api       *webrtc.API
	gatherer  *webrtc.ICEGatherer
	transport *webrtc.ICETransport
	agent     *ice.Agent
	conn      *ice.Conn
	role      webrtc.ICERole

	handoverMu   sync.Mutex
	gatherFilter *candidateGatherFilter

	remoteMu              sync.RWMutex
	remoteCandidates      map[string]ice.Candidate
	publishLocalCandidate func(webrtc.ICECandidate) error
}

type endpointConfig struct {
	Controlling bool
	ICEServers  []webrtc.ICEServer
}

func newManualEndpoint(cfg endpointConfig) (*manualEndpoint, error) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	loggerFactory.DefaultLogLevel = logging.LogLevelInfo

	gatherFilter := newCandidateGatherFilter()
	agentOpts := []ice.AgentOption{
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4}),
		ice.WithInterfaceFilter(gatherFilter.Keep),
		ice.WithIPFilter(isIPv4),
		ice.WithRemoteIPFilter(isIPv4),
		ice.WithLoggerFactory(loggerFactory),
		ice.WithContinualGatheringPolicy(ice.GatherContinually),
		ice.WithNetworkMonitorInterval(250 * time.Millisecond),
	}
	if !cfg.Controlling {
		agentOpts = append(agentOpts, ice.WithBindingRequestHandler(acceptRelayRenominationRequest))
	}
	agentOpts = append(agentOpts, ice.WithRenomination(ice.DefaultNominationValueGenerator()))

	api := webrtc.NewAPI()
	gatherer, err := api.NewICEGatherer(webrtc.ICEGatherOptions{
		ICEServers: cfg.ICEServers,
	})
	if err != nil {
		return nil, fmt.Errorf("create ICE gatherer: %w", err)
	}

	iceServerURLs, err := iceGathererValidatedServers(gatherer)
	if err != nil {
		return nil, err
	}
	agentOpts = append(agentOpts, ice.WithUrls(iceServerURLs))

	agent, err := ice.NewAgentWithOptions(agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("create ICE agent: %w", err)
	}

	if err := injectCustomAgent(gatherer, agent); err != nil {
		return nil, err
	}

	transport := api.NewICETransport(gatherer)
	role := webrtc.ICERoleControlled
	if cfg.Controlling {
		role = webrtc.ICERoleControlling
	}

	ep := &manualEndpoint{
		api:              api,
		gatherer:         gatherer,
		transport:        transport,
		agent:            agent,
		role:             role,
		gatherFilter:     gatherFilter,
		remoteCandidates: map[string]ice.Candidate{},
	}

	if err := ep.installCallbacks(); err != nil {
		return nil, err
	}

	return ep, nil
}

func (e *manualEndpoint) installCallbacks() error {
	e.gatherer.OnLocalCandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			log.Printf("local candidate gathering complete")
			return
		}
		if e.publishLocalCandidate == nil {
			return
		}
		if err := e.publishLocalCandidate(*c); err != nil {
			log.Printf("send candidate failed: %v", err)
		}
	})

	if err := e.agent.OnConnectionStateChange(func(state ice.ConnectionState) {
		log.Printf("ICE state: %s", state)
	}); err != nil {
		return err
	}

	if err := e.agent.OnSelectedCandidatePairChange(func(local, remote ice.Candidate) {
		localIface, _ := interfaceNameForIP(local.Address())
		log.Printf("selected pair local=%s(iface=%s, prio=%d) remote=%s",
			local.Address(), localIface, local.Priority(), remote.Address())
	}); err != nil {
		return err
	}

	e.transport.OnConnectionStateChange(func(state webrtc.ICETransportState) {
		log.Printf("ICE transport state: %s", state)
	})

	return nil
}

func (e *manualEndpoint) SetLocalCandidatePublisher(fn func(webrtc.ICECandidate) error) {
	e.publishLocalCandidate = fn
}

func (e *manualEndpoint) Close() error {
	if e.transport != nil {
		return e.transport.GracefulStop()
	}
	if e.agent != nil {
		return e.agent.GracefulClose()
	}
	return nil
}

func (e *manualEndpoint) StartGathering() error {
	return e.gatherer.Gather()
}

func (e *manualEndpoint) LocalParameters() (webrtc.ICEParameters, error) {
	return e.gatherer.GetLocalParameters()
}

func (e *manualEndpoint) StartTransport(ctx context.Context, remote webrtc.ICEParameters) error {
	var (
		conn *ice.Conn
		err  error
	)

	switch e.role {
	case webrtc.ICERoleControlling:
		conn, err = e.agent.Dial(ctx, remote.UsernameFragment, remote.Password)
	case webrtc.ICERoleControlled:
		conn, err = e.agent.Accept(ctx, remote.UsernameFragment, remote.Password)
	default:
		err = fmt.Errorf("unknown ICE role: %s", e.role)
	}
	if err != nil {
		return err
	}

	e.conn = conn
	return nil
}

func (e *manualEndpoint) Conn() *ice.Conn {
	return e.conn
}

func (e *manualEndpoint) AddRemoteCandidate(c webrtc.ICECandidate) error {
	remote, err := c.ToICE()
	if err != nil {
		return fmt.Errorf("convert remote candidate: %w", err)
	}
	if !isIPv4(net.ParseIP(remote.Address())) {
		log.Printf("skip non-IPv4 remote candidate addr=%s type=%s", remote.Address(), remote.Type())
		return nil
	}

	e.remoteMu.Lock()
	e.remoteCandidates[remote.ID()] = remote
	e.remoteMu.Unlock()

	if err := e.agent.AddRemoteCandidate(remote); err != nil {
		return fmt.Errorf("add remote candidate: %w", err)
	}

	return nil
}

func isIPv4(ip net.IP) bool {
	return ip != nil && ip.To4() != nil
}

func acceptRelayRenominationRequest(msg *stun.Message, local, _ ice.Candidate, _ *ice.CandidatePair) bool {
	return msg != nil &&
		local != nil &&
		local.Type() == ice.CandidateTypeRelay &&
		msg.Contains(ice.DefaultNominationAttribute)
}

func (e *manualEndpoint) StartCellularRelayHandover(ctx context.Context) error {
	if e.role != webrtc.ICERoleControlling {
		return errors.New("manual renomination requires the controlling side")
	}

	cell, err := firstActiveCellularAddress()
	if err != nil {
		return err
	}

	e.handoverMu.Lock()
	defer e.handoverMu.Unlock()

	log.Printf("starting cellular relay handover if=%s ip=%s", cell.Name, cell.IP)
	e.gatherFilter.UseCellularOnly()
	if err := prepareAgentForCellularRelayGathering(e.agent); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		local, remote, err := e.findCellularRelayPair()
		if err != nil {
			return err
		}
		if local != nil && remote != nil {
			log.Printf("forcing relay handover via local=%s remote=%s", local, remote)
			return e.agent.RenominateCandidate(local, remote)
		}

		select {
		case <-waitCtx.Done():
			return errors.New("no cellular relay to remote relay candidate pair found")
		case <-ticker.C:
		}
	}
}

func (e *manualEndpoint) findCellularRelayPair() (ice.Candidate, ice.Candidate, error) {
	localCandidates, err := e.agent.GetLocalCandidates()
	if err != nil {
		return nil, nil, fmt.Errorf("list local candidates: %w", err)
	}

	locals := make(map[string]ice.Candidate, len(localCandidates))
	for _, cand := range localCandidates {
		locals[cand.ID()] = cand
	}

	e.remoteMu.RLock()
	remotes := make(map[string]ice.Candidate, len(e.remoteCandidates))
	for id, cand := range e.remoteCandidates {
		remotes[id] = cand
	}
	e.remoteMu.RUnlock()

	stats := e.agent.GetCandidatePairsStats()
	var (
		handoverLocal  ice.Candidate
		handoverRemote ice.Candidate
	)

	for _, stat := range stats {
		local := locals[stat.LocalCandidateID]
		remote := remotes[stat.RemoteCandidateID]
		if local == nil || remote == nil {
			continue
		}

		localIface, localIfaceOK := candidateInterfaceName(local)
		localIsCellularRelay := local.Type() == ice.CandidateTypeRelay && localIfaceOK && isCellularLikeInterface(localIface)
		remoteIsRelay := remote.Type() == ice.CandidateTypeRelay

		if !localIsCellularRelay || !remoteIsRelay {
			continue
		}

		handoverLocal = local
		handoverRemote = remote
		if stat.State == ice.CandidatePairStateSucceeded || stat.Nominated {
			break
		}
	}

	return handoverLocal, handoverRemote, nil
}

func candidateInterfaceName(c ice.Candidate) (string, bool) {
	if c == nil {
		return "", false
	}

	if related := c.RelatedAddress(); related != nil && related.Address != "" {
		if ifaceName, err := interfaceNameForIP(related.Address); err == nil {
			return ifaceName, true
		}
	}

	if ifaceName, err := interfaceNameForIP(c.Address()); err == nil {
		return ifaceName, true
	}

	return "", false
}

type candidateGatherMode int

const (
	gatherNonCellular candidateGatherMode = iota
	gatherCellularOnly
)

type candidateGatherFilter struct {
	mu   sync.RWMutex
	mode candidateGatherMode
}

func newCandidateGatherFilter() *candidateGatherFilter {
	return &candidateGatherFilter{mode: gatherNonCellular}
}

func (f *candidateGatherFilter) Keep(ifaceName string) bool {
	f.mu.RLock()
	mode := f.mode
	f.mu.RUnlock()

	isCellular := isCellularLikeInterface(ifaceName)
	switch mode {
	case gatherCellularOnly:
		return isCellular
	default:
		return !isCellular
	}
}

func (f *candidateGatherFilter) UseCellularOnly() {
	f.mu.Lock()
	f.mode = gatherCellularOnly
	f.mu.Unlock()
}

func firstActiveCellularAddress() (InterfaceAddress, error) {
	addrs, err := ListActiveInterfaceAddrs()
	if err != nil {
		return InterfaceAddress{}, err
	}
	for _, addr := range addrs {
		if addr.IsCellularLike {
			return addr, nil
		}
	}
	return InterfaceAddress{}, errors.New("no active cellular interface with an IPv4 address found")
}

func prepareAgentForCellularRelayGathering(agent *ice.Agent) error {
	return runOnAgentLoop(agent, func() error {
		if err := setAgentCandidateTypes(agent, []ice.CandidateType{ice.CandidateTypeRelay}); err != nil {
			return err
		}
		return resetAgentLastKnownInterfaces(agent)
	})
}

func runOnAgentLoop(agent *ice.Agent, fn func() error) error {
	v := reflect.ValueOf(agent)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("nil ICE agent")
	}

	field := v.Elem().FieldByName("loop")
	if !field.IsValid() {
		return errors.New("ICE agent loop field not found")
	}

	loop := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	run := loop.MethodByName("Run")
	if !run.IsValid() {
		return errors.New("ICE agent loop Run method not found")
	}

	var fnErr error
	fnType := run.Type().In(1)
	task := reflect.MakeFunc(fnType, func([]reflect.Value) []reflect.Value {
		fnErr = fn()
		return nil
	})

	out := run.Call([]reflect.Value{reflect.ValueOf(context.Background()), task})
	if len(out) == 1 && !out[0].IsNil() {
		return out[0].Interface().(error)
	}
	return fnErr
}

func setAgentCandidateTypes(agent *ice.Agent, types []ice.CandidateType) error {
	v := reflect.ValueOf(agent)
	field := v.Elem().FieldByName("candidateTypes")
	if !field.IsValid() {
		return errors.New("ICE agent candidateTypes field not found")
	}

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(types))
	return nil
}

func resetAgentLastKnownInterfaces(agent *ice.Agent) error {
	v := reflect.ValueOf(agent)
	field := v.Elem().FieldByName("lastKnownInterfaces")
	if !field.IsValid() {
		return errors.New("ICE agent lastKnownInterfaces field not found")
	}

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.MakeMap(field.Type()))
	return nil
}

func injectCustomAgent(g *webrtc.ICEGatherer, agent *ice.Agent) error {
	v := reflect.ValueOf(g)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("nil ICEGatherer")
	}

	elem := v.Elem()
	field := elem.FieldByName("agent")
	if !field.IsValid() {
		return errors.New("ICEGatherer.agent field not found")
	}

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(agent))
	return nil
}

func iceGathererValidatedServers(g *webrtc.ICEGatherer) ([]*stun.URI, error) {
	v := reflect.ValueOf(g)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil, errors.New("nil ICEGatherer")
	}

	elem := v.Elem()
	field := elem.FieldByName("validatedServers")
	if !field.IsValid() {
		return nil, errors.New("ICEGatherer.validatedServers field not found")
	}

	urls, ok := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().([]*stun.URI)
	if !ok {
		return nil, errors.New("ICEGatherer.validatedServers field has unexpected type")
	}
	return urls, nil
}
