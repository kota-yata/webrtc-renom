GOCACHE ?= $(CURDIR)/.gocache
export GOCACHE
GOOS ?= $(shell go env GOOS 2>/dev/null)
GO_LDFLAGS ?= $(if $(filter android,$(GOOS)),-checklinkname=0,)
GO_LDFLAGS_ARG := $(if $(strip $(GO_LDFLAGS)),-ldflags '$(GO_LDFLAGS)',)

SIGNAL_ADDR ?= 0.0.0.0:7893
SERVER_URL ?= https://203.178.143.72:7893
TLS_CERT := $(CURDIR)/cert/server.crt
TLS_KEY := $(CURDIR)/cert/server.key
TLS_CA_CERT := $(TLS_CERT)
CERT_CN ?= 203.178.143.72
CERT_SAN ?= IP:203.178.143.72,IP:127.0.0.1,DNS:localhost
ICE_SERVERS := turn:203.178.143.72:3478?transport=udp
ICE_USERNAME := cmp9
ICE_CREDENTIAL := dc+zjBYP+nf+bC6gXvliLKNgzu8lR3XO
POLL_TIMEOUT ?= 25s

.PHONY: build cert check-tls-ca-cert peer run-signal run-controlling-peer run-controlled-peer clean

build:
	go build $(GO_LDFLAGS_ARG) ./cmd/peer
	go build $(GO_LDFLAGS_ARG) ./cmd/signaling-server

cert: $(TLS_CERT) $(TLS_KEY)

$(TLS_CERT) $(TLS_KEY):
	mkdir -p $(CURDIR)/cert
	openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
		-keyout $(TLS_KEY) \
		-out $(TLS_CERT) \
		-subj /CN=$(CERT_CN) \
		-addext subjectAltName=$(CERT_SAN)

check-tls-ca-cert:
	test -f $(TLS_CA_CERT) || (echo "missing $(TLS_CA_CERT); copy cert/server.crt from the signaling server first" && false)

peer: check-tls-ca-cert
	go run $(GO_LDFLAGS_ARG) ./cmd/peer \
		--server-url $(SERVER_URL) \
		--tls-ca-cert $(TLS_CA_CERT) \
		--peer-id $(PEER_ID) \
		--remote-peer-id $(REMOTE_PEER_ID) \
		--ice-servers '$(ICE_SERVERS)' \
		--ice-username '$(ICE_USERNAME)' \
		--ice-credential '$(ICE_CREDENTIAL)' \
		--poll-timeout $(POLL_TIMEOUT) \
		$(if $(CONTROLLING),--controlling,)

run-signal: cert
	go run $(GO_LDFLAGS_ARG) ./cmd/signaling-server --listen $(SIGNAL_ADDR) \
		--tls-cert $(TLS_CERT) \
		--tls-key $(TLS_KEY)

run-controlling-peer:
	$(MAKE) peer PEER_ID=peer-a REMOTE_PEER_ID=peer-b CONTROLLING=1

run-controlled-peer:
	$(MAKE) peer PEER_ID=peer-b REMOTE_PEER_ID=peer-a

clean:
	rm -rf $(GOCACHE) peer signaling-server
