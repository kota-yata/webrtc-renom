GO ?= go
GOCACHE ?= $(CURDIR)/.gocache

SIGNAL_ADDR ?= 127.0.0.1:8080
SERVER_URL ?= http://127.0.0.1:8080
SESSION_ID ?= demo
WIFI_IFACE ?=
RSSI_THRESHOLD ?= -75
RSSI_HYSTERESIS ?= 4
GATHER_IFACES ?=
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
		--rssi-threshold $(RSSI_THRESHOLD) \
		--rssi-hysteresis $(RSSI_HYSTERESIS) \
		--gather-ifaces $(GATHER_IFACES) \
		--poll-timeout $(POLL_TIMEOUT) \
		$(if $(CONTROLLING),--controlling,)

run-signal: signaling-server

run-peer-a:
	$(MAKE) peer PEER_ID=peer-a REMOTE_PEER_ID=peer-b CONTROLLING=1

run-peer-b:
	$(MAKE) peer PEER_ID=peer-b REMOTE_PEER_ID=peer-a

clean:
	rm -rf $(GOCACHE) peer signaling-server
