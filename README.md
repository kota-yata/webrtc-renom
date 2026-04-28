## WebRTC application for QSwitch evaluation
- Rewrite candidate priority when netlink detects Wi-Fi address removal
- Force renomination to a cellular-backed local relay candidate and a remote relay candidate
- Expected to run on Android devices

Wi-Fi disable/disconnect is detected through netlink address updates.
TURN URLs can be supplied with `--ice-servers`, `--ice-username`, and `--ice-credential`.

## Run
```sh
make run-signal
make run-controlling-peer WIFI_IFACE=wlan0 GATHER_IFACES=wlan0,rmnet0
make run-controlled-peer WIFI_IFACE=wlan0 GATHER_IFACES=wlan0,rmnet0
```
