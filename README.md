# zx909-gw

Production-oriented **Go** TCP gateway for Chinese **ZX909 / C09 / Topin-family** pet GPS trackers (GT06-derived binary protocol).

It accepts long-lived TCP connections from trackers, parses login / GPS / LBS / Wi‑Fi / status packets, replies with the must-ACK frames those firmwares expect, and forwards data to **ThingsBoard CE** via the **MQTT Gateway API** (`v1/gateway/*`).

---

## Architecture

```text
┌─────────────────┐     TCP :8002      ┌──────────────────────────────────────┐
│  ZX909 / C09    │ ─────────────────► │              zx909-gw                │
│  (pet trackers) │ ◄───────────────── │  protocol/  frame + parsers + ACKs   │
└─────────────────┘   binary ACKs      │  server/    sessions by IMEI         │
                                       │  command/   RPC → binary downlink    │
                                       │  mqtt/      Gateway API client       │
                                       │  geolocation/  optional LBS→lat/lon  │
                                       └───────────────┬──────────────────────┘
                                                       │ MQTT
                                                       │ v1/gateway/telemetry
                                                       │ v1/gateway/connect
                                                       │ v1/gateway/rpc
                                                       ▼
                                               ┌───────────────┐
                                               │ ThingsBoard CE│
                                               │ Gateway device│
                                               │ + child IMEIs │
                                               └───────────────┘
```

**ThingsBoard model**

- One **Gateway** device in TB (access token used as MQTT username).
- Each tracker is a **child device** named by IMEI (created on `v1/gateway/connect` or pre-provisioned).
- Telemetry is published per child device.
- Server-side RPC to a child device is delivered on `v1/gateway/rpc`; the gateway executes it and publishes the response on the same topic.

**In-process design**

| Piece | Role |
|-------|------|
| `server.Server` | TCP accept loop, per-IMEI sessions, frame dispatch |
| `SafeConn` | Serialises all writes on a connection (ACKs + commands) |
| `command.Handler` | Single source of truth for downlink (`reboot`, `locate`, intervals, …) |
| `mqtt.Client` | Interface: mock or real Gateway client |
| `geolocation.Client` | Optional HTTP resolve of Wi‑Fi/LBS → coordinates |

Debug REST (`server.debug_api`) and MQTT RPC both call `command.Handler`, so behaviour stays consistent.

---

## Protocol

Primary reference: **365GPS 2G/4G** (Topin family). Classic Concox GT06 is only a baseline for framing ideas; live ZX909_EU traffic often uses **dummy length bytes** and **no CRC** on short packets. The parser is **trailer-based** (`78 78` … `0D 0A`).

### Frame shape

```text
78 78 | length (often dummy) | protocol | body… | 0D 0A
```

### Handled message types

| Hex | Name | Direction | Gateway behaviour |
|-----|------|-----------|-------------------|
| `0x01` | Login | C→S | Parse IMEI (BCD), register session, TB `connect`, ACK `787801010d0a` |
| `0x30` | Time sync | C→S | Reply UTC (`year` = uint16 BE) |
| `0x57` | Settings request | C→S | Reply default settings blob |
| `0x13` | Status | C→S | Parse battery (etc.), publish `battery`, echo frame as ACK |
| `0x10` / `0x11` | GPS online / offline | C→S | Parse fix, publish location, datetime ACK |
| `0x12` / `0x22` | GPS variants | C→S | Same GPS path when body matches |
| `0x1A` / `0x1B` | Wi‑Fi + LBS | C→S | Parse radio data; optional geolocation; datetime ACK |
| `0x17` / `0x69` | Offline / online Wi‑Fi+LBS | C→S | Same family as `0x1A` |
| `0xB3` | ICCID | C→S | Publish `iccid` (no ACK) |
| `0x48` | Restart / shutdown | S→C | `01` reboot, `02` shutdown |
| `0x97` | Location upload interval | S→C | Seconds `10–7200` |
| `0x80` | Force locate | S→C | Immediate position request |

**Must-reply** (device reconnects if ignored): `0x01`, `0x10`, `0x11`, `0x13`, `0x17`, `0x69` (and related datetime ACKs).

### Telemetry shape

**GPS**

```json
{
  "position_type": "gps",
  "latitude": 35.6892,
  "longitude": 51.3890,
  "speed": 5.0,
  "course": 90,
  "satellites": 12,
  "valid": true
}
```

**LBS / Wi‑Fi** (only if `geolocation.enabled` and the HTTP API succeeds)

```json
{
  "position_type": "lbs_wifi",
  "latitude": 35.6892,
  "longitude": 51.3890
}
```

**Status:** `{ "battery": 100 }` · **SIM:** `{ "iccid": "…" }`

LBS frames are always ACKed immediately; geolocation runs asynchronously and never blocks the device.

### Downlink RPC (ThingsBoard → device)

| Method | Params | Binary |
|--------|--------|--------|
| `reboot` | `{}` | `0x48 01` |
| `shutdown` | `{}` | `0x48 02` |
| `locate` | `{}` | `0x80` |
| `setLocationInterval` | `{"seconds": 60}` | `0x97` |
| `setStatusInterval` | `{"minutes": 5}` | status form of `0x13` |

---

## Configuration

Copy the example and edit:

```bash
cp configs/config.example.yaml config.yaml
```

```yaml
server:
  listen: ":8002"
  read_timeout: 5m
  write_timeout: 10s
  # Optional command injector (keep on localhost in production)
  debug_api: "127.0.0.1:8090"

thingsboard:
  host: "localhost"
  port: 1883
  # Typical CE: username = access token, password empty
  access_token: "YOUR_GATEWAY_TOKEN"
  client_id: "zx909-gw"
  password: ""
  device_profile: "pet-tracker"
  qos: 1
  keepalive: 30s
  # true  = no broker (protocol / parser work)
  # false = real MQTT Gateway API
  use_mock: true

geolocation:
  enabled: false
  url: "https://location.example.com/v1/geolocate"
  api_key: ""
  timeout: 3s

logging:
  level: info   # debug | info | warn | error
```

| Key | Meaning |
|-----|---------|
| `server.listen` | TCP bind for trackers |
| `server.debug_api` | HTTP bind for debug commands; empty disables |
| `thingsboard.use_mock` | Mock MQTT vs real broker |
| `thingsboard.access_token` | Gateway device token (MQTT username) |
| `geolocation.enabled` | If false: no HTTP call and no LBS location telemetry |

### Debug REST (when enabled)

```text
GET  /devices
GET  /health
POST /devices/{imei}/reboot
POST /devices/{imei}/shutdown
POST /devices/{imei}/locate
POST /devices/{imei}/interval   JSON: {"location_seconds":60,"status_minutes":5}
```

---

## Build & run

### Local (Go 1.24+)

```bash
go build -o gateway ./cmd/gateway
./gateway -config config.yaml
```

Or:

```bash
go run ./cmd/gateway -config config.yaml
```

### Docker

```bash
docker build -t zx909-gw:latest .
# If Alpine/DNS is flaky in your environment:
# docker build --network=host -t zx909-gw:latest .

docker run --rm -p 8002:8002 \
  -v "$PWD/config.yaml:/app/configs/config.yaml:ro" \
  zx909-gw:latest
```

Image is multi-stage: static binary on `distroless`. Override config with a bind mount. For ThingsBoard on the host, use a reachable hostname (not `localhost` inside the container unless you use host networking).

### Toy device (simulator)

```bash
go run ./cmd/toy -addr 127.0.0.1:8002 -imei 861971080061529
```

Sends login, time-sync, GPS every 30s (eastbound 5 km/h), status every 2 minutes.

### Point a real tracker

SMS (vendor-dependent), for example:

```text
server#YOUR.PUBLIC.IP#8002#
```

---

## ThingsBoard setup (short)

1. Create a device, enable **Is gateway**, copy the access token into `thingsboard.access_token`.
2. Set `use_mock: false`, point `host`/`port` at the MQTT broker.
3. Start the gateway; on tracker login you should see a child device named with the IMEI.
4. Send RPC (two-way) to the child device, e.g. method `reboot` with params `{}`.

See `command_curl_examples.txt` for sample REST calls.

---

## Project layout

```text
cmd/gateway/           main entrypoint
cmd/toy/               simple tracker simulator
internal/
  config/              YAML loading
  protocol/            framing, parsers, ACK/command builders
  server/              TCP sessions, SafeConn, debug REST
  command/             RPC method → binary payload
  mqtt/                Gateway MQTT client + mock
  geolocation/         optional LBS/Wi-Fi HTTP client
configs/               example configuration
Dockerfile
```

---

## Out of scope (for now)

- Official ThingsBoard IoT Gateway (Python)
- Full SMS-style string command protocol over TCP
- Production auth on the debug REST API
- Mid-body `0D0A` framing hardening

---

## License / repo

Private repository. Do not publish access tokens or live broker credentials in config committed to git.
