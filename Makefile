GO ?= go
GOCACHE ?= $(CURDIR)/.gocache

SIGNAL_ADDR ?= 203.178.143.72:8080
SERVER_URL ?= http://203.178.143.72:8080
SESSION_ID ?= demo
WIFI_IFACE ?=
GATHER_IFACES ?=
ICE_SERVERS ?=
ICE_USERNAME ?=
ICE_CREDENTIAL ?=
POLL_TIMEOUT ?= 25s

.PHONY: build signaling-server peer run-signal run-peer-a run-peer-b clean

build:
	GOCACHE=$(GOCACHE) $(GO) build ./cmd/peer
	GOCACHE=$(GOCACHE) $(GO) build ./cmd/signaling-server

signaling-server:
	GOCACHE=$(GOCACHE) $(GO) run ./cmd/signaling-server --listen $(SIGNAL_ADDR)

peer:
	GOCACHE=$(GOCACHE) $(GO) run ./cmd/peer \
		--server-url $(SERVER_URL) \
		--session-id $(SESSION_ID) \
		--peer-id $(PEER_ID) \
		--remote-peer-id $(REMOTE_PEER_ID) \
		--wifi-iface $(WIFI_IFACE) \
		--gather-ifaces $(GATHER_IFACES) \
		--ice-servers $(ICE_SERVERS) \
		--ice-username $(ICE_USERNAME) \
		--ice-credential $(ICE_CREDENTIAL) \
		--poll-timeout $(POLL_TIMEOUT) \
		$(if $(CONTROLLING),--controlling,)

run-signal: signaling-server

run-peer-a:
	$(MAKE) peer PEER_ID=peer-a REMOTE_PEER_ID=peer-b CONTROLLING=1

run-peer-b:
	$(MAKE) peer PEER_ID=peer-b REMOTE_PEER_ID=peer-a

clean:
	rm -rf $(GOCACHE) peer signaling-server
