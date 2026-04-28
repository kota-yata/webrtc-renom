## WebRTC application for QSwitch evaluation
- Rewrite candidate priority when netlink detects Wi-Fi address removal
- Force renomination to a cellular-backed local relay candidate and a remote relay candidate
- Expected to run on Android devices

Wi-Fi disable/disconnect is detected through netlink address updates.
TURN URLs can be supplied with `--ice-servers`, `--ice-username`, and `--ice-credential`.

## Run
On the signaling server:
```sh
make cert
make run-signal
```

On each peer:
```sh
make run-controlling-peer
make run-controlled-peer
```
