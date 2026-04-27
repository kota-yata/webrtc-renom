## WebRTC application for QSwitch evaluation
- Rewrite candidate priority when netlink detects Wi-Fi address removal
- Force renomination to a cellular-backed local relay candidate and a remote relay candidate
- Expected to run on Android devices

Wi-Fi disable/disconnect is detected through netlink address updates.
TURN URLs can be supplied with `--ice-servers`, `--ice-username`, and `--ice-credential`.

## Run

Start the signaling server:

```sh
go run ./cmd/signaling-server --listen 203.178.143.72:8080
```

Start the controlling peer:

```sh
go run ./cmd/peer \
  --controlling \
  --server-url http://203.178.143.72:8080 \
  --session-id demo \
  --peer-id peer-a \
  --remote-peer-id peer-b \
  --wifi-iface wlan0 \
  --gather-ifaces wlan0,rmnet0 \
  --ice-servers turn:turn.example.com:3478?transport=udp \
  --ice-username user \
  --ice-credential pass
```

Start the controlled peer:

```sh
go run ./cmd/peer \
  --server-url http://203.178.143.72:8080 \
  --session-id demo \
  --peer-id peer-b \
  --remote-peer-id peer-a \
  --wifi-iface wlan0 \
  --gather-ifaces wlan0,rmnet0 \
  --ice-servers turn:turn.example.com:3478?transport=udp \
  --ice-username user \
  --ice-credential pass
```

The same setup can be run through the Makefile:

```sh
make run-signal
make run-peer-a WIFI_IFACE=wlan0 GATHER_IFACES=wlan0,rmnet0 ICE_SERVERS='turn:turn.example.com:3478?transport=udp' ICE_USERNAME=user ICE_CREDENTIAL=pass
make run-peer-b WIFI_IFACE=wlan0 GATHER_IFACES=wlan0,rmnet0 ICE_SERVERS='turn:turn.example.com:3478?transport=udp' ICE_USERNAME=user ICE_CREDENTIAL=pass
```
