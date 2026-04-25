package renom

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"unsafe"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

type manualEndpoint struct {
	api       *webrtc.API
	gatherer  *webrtc.ICEGatherer
	transport *webrtc.ICETransport
	agent     *ice.Agent
	role      webrtc.ICERole

	remoteMu              sync.RWMutex
	remoteCandidates      map[string]ice.Candidate
	publishLocalCandidate func(webrtc.ICECandidate) error
}

type endpointConfig struct {
	Controlling      bool
	GatherInterfaces []string
}

func newManualEndpoint(cfg endpointConfig) (*manualEndpoint, error) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	loggerFactory.DefaultLogLevel = logging.LogLevelInfo

	interfaceFilter := func(name string) bool {
		if len(cfg.GatherInterfaces) == 0 {
			return true
		}
		for _, allowed := range cfg.GatherInterfaces {
			if name == allowed {
				return true
			}
		}
		return false
	}

	agentOpts := []ice.AgentOption{
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4, ice.NetworkTypeUDP6}),
		ice.WithInterfaceFilter(interfaceFilter),
		ice.WithLoggerFactory(loggerFactory),
	}
	if cfg.Controlling {
		agentOpts = append(agentOpts, ice.WithRenomination(ice.DefaultNominationValueGenerator()))
	}

	agent, err := ice.NewAgentWithOptions(agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("create ICE agent: %w", err)
	}

	api := webrtc.NewAPI()
	gatherer, err := api.NewICEGatherer(webrtc.ICEGatherOptions{})
	if err != nil {
		return nil, fmt.Errorf("create ICE gatherer: %w", err)
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

func (e *manualEndpoint) StartGathering() error {
	return e.gatherer.Gather()
}

func (e *manualEndpoint) LocalParameters() (webrtc.ICEParameters, error) {
	return e.gatherer.GetLocalParameters()
}

func (e *manualEndpoint) StartTransport(remote webrtc.ICEParameters) error {
	return e.transport.Start(nil, remote, &e.role)
}

func (e *manualEndpoint) AddRemoteCandidate(c webrtc.ICECandidate) error {
	remote, err := c.ToICE()
	if err != nil {
		return fmt.Errorf("convert remote candidate: %w", err)
	}

	e.remoteMu.Lock()
	e.remoteCandidates[remote.ID()] = remote
	e.remoteMu.Unlock()

	if err := e.agent.AddRemoteCandidate(remote); err != nil {
		return fmt.Errorf("add remote candidate: %w", err)
	}

	return nil
}

func (e *manualEndpoint) ForceHandoverToCellular() error {
	if e.role != webrtc.ICERoleControlling {
		return errors.New("manual renomination requires the controlling side")
	}

	localCandidates, err := e.agent.GetLocalCandidates()
	if err != nil {
		return fmt.Errorf("list local candidates: %w", err)
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
		bestLocal  ice.Candidate
		bestRemote ice.Candidate
		bestScore  int64 = -1
	)

	for _, stat := range stats {
		if stat.State != ice.CandidatePairStateSucceeded {
			continue
		}

		local := locals[stat.LocalCandidateID]
		remote := remotes[stat.RemoteCandidateID]
		if local == nil || remote == nil {
			continue
		}

		ifaceName, _ := interfaceNameForIP(local.Address())
		isCellularLike := isCellularLikeInterface(ifaceName)
		score := int64(local.Priority())
		if isCellularLike {
			score += 1 << 32
		}
		if stat.Nominated {
			score += 1 << 20
		}

		log.Printf("candidate pair local=%s iface=%s remote=%s isCellularLike=%t score=%d",
			local.Address(), ifaceName, remote.Address(), isCellularLike, score)

		if score > bestScore {
			bestScore = score
			bestLocal = local
			bestRemote = remote
		}
	}

	if bestLocal == nil || bestRemote == nil {
		return errors.New("no succeeded candidate pair matched the cellular policy")
	}

	log.Printf("forcing handover via local=%s remote=%s", bestLocal, bestRemote)
	return e.agent.RenominateCandidate(bestLocal, bestRemote)
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
