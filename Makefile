GOCACHE ?= $(CURDIR)/.gocache

SIGNAL_ADDR ?= 0.0.0.0:7893
SERVER_URL ?= https://203.178.143.72:7893
TLS_CERT := $(CURDIR)/cert/server.crt
TLS_KEY := $(CURDIR)/cert/server.key
TLS_CA_CERT := $(TLS_CERT)
CERT_CN ?= 203.178.143.72
CERT_SAN ?= IP:203.178.143.72,IP:127.0.0.1,DNS:localhost
SESSION_ID ?= demo
WIFI_IFACE ?=
GATHER_IFACES ?=
ICE_SERVERS := turn:203.178.143.72:3478?transport=udp
ICE_USERNAME := cmp9
ICE_CREDENTIAL := dc+zjBYP+nf+bC6gXvliLKNgzu8lR3XO
POLL_TIMEOUT ?= 25s

.PHONY: build cert peer run-signal run-controlling-peer run-controlled-peer clean

build:
	go build ./cmd/peer
	go build ./cmd/signaling-server

cert:
	mkdir -p $(CURDIR)/cert
	test -f $(TLS_CERT) -a -f $(TLS_KEY) || openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
		-keyout $(TLS_KEY) \
		-out $(TLS_CERT) \
		-subj /CN=$(CERT_CN) \
		-addext subjectAltName=$(CERT_SAN)

peer: cert
	go run ./cmd/peer \
		--server-url $(SERVER_URL) \
		--tls-ca-cert $(TLS_CA_CERT) \
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

run-signal: cert
	go run ./cmd/signaling-server --listen $(SIGNAL_ADDR) \
		--tls-cert $(TLS_CERT) \
		--tls-key $(TLS_KEY)

run-controlling-peer:
	$(MAKE) peer PEER_ID=peer-a REMOTE_PEER_ID=peer-b CONTROLLING=1

run-controlled-peer:
	$(MAKE) peer PEER_ID=peer-b REMOTE_PEER_ID=peer-a

clean:
	rm -rf $(GOCACHE) peer signaling-server
