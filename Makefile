GOCACHE ?= $(CURDIR)/.gocache

SIGNAL_ADDR ?= 0.0.0.0:7893
SERVER_URL ?= http://203.178.143.72:7893
SESSION_ID ?= demo
WIFI_IFACE ?=
GATHER_IFACES ?=
ICE_SERVERS := turn:203.178.143.72:3478?transport=udp
ICE_USERNAME := cmp9
ICE_CREDENTIAL := dc+zjBYP+nf+bC6gXvliLKNgzu8lR3XO
POLL_TIMEOUT ?= 25s

.PHONY: build peer run-signal run-controlling-peer run-controlled-peer clean

build:
	go build ./cmd/peer
	go build ./cmd/signaling-server

peer:
	go run ./cmd/peer \
		--server-url $(SERVER_URL) \
		--session-id $(SESSION_ID) \
		--peer-id $(PEER_ID) \
		--remote-peer-id $(REMOTE_PEER_ID) \
		--wifi-iface $(WIFI_IFACE) \
		--gather-ifaces $(GATHER_IFACES) \
		--ice-servers '$(ICE_SERVERS)' \
		--ice-username '$(ICE_USERNAME)' \
		--ice-credential '$(ICE_CREDENTIAL)' \
		--poll-timeout $(POLL_TIMEOUT) \
		$(if $(CONTROLLING),--controlling,)

run-signal:
	go run ./cmd/signaling-server --listen $(SIGNAL_ADDR)

run-controlling-peer:
	$(MAKE) peer PEER_ID=peer-a REMOTE_PEER_ID=peer-b CONTROLLING=1

run-controlled-peer:
	$(MAKE) peer PEER_ID=peer-b REMOTE_PEER_ID=peer-a

clean:
	rm -rf $(GOCACHE) peer signaling-server
