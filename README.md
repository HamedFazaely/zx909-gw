# zx909-gw

Production-oriented TCP → ThingsBoard MQTT Gateway for **Topin ZX909 / C09** (and similar GT06-derived) pet GPS trackers.

## Architecture

```
[ZX909 / C09 trackers]  ──TCP :8002──►  zx909-gw  ──MQTT Gateway API──►  ThingsBoard
                                              │
                                              ├── protocol/   frame + CRC + parsers
                                              ├── server/     TCP sessions by IMEI
                                              └── mqtt/       v1/gateway/* topics
```

- One **Gateway device** in ThingsBoard (flag *Is gateway*).
- Child devices are auto-created (or pre-provisioned) using IMEI as device name.
- Trackers keep long-lived TCP connections; the gateway replies with correct ACKs so they stay online.

## Quick start

1. Create a device in ThingsBoard, enable **Is gateway**, copy its access token.
2. Copy config:

   ```bash
   cp configs/config.example.yaml config.yaml
   # edit host, access_token, listen port
   ```

3. Run:

   ```bash
   go run ./cmd/gateway -config config.yaml
   ```

4. Point a tracker at the gateway (SMS):

   ```
   server#YOUR.PUBLIC.IP#8002#
   ```

5. Watch logs for login + GPS packets and check the child device in ThingsBoard.

## Project layout

```
cmd/gateway/          main entrypoint
internal/
  config/             YAML + env loading
  protocol/           GT06/Topin framing, CRC-16/X25, packet types
  server/             TCP listener + per-IMEI sessions
  mqtt/               ThingsBoard Gateway MQTT client
pkg/                  (reserved)
configs/              example configuration
```

## Protocol notes

ZX909_EU firmware speaks a GT06 / Topin-derived binary protocol:

- Frame: `78 78 | LEN | PROTO | BODY | SERIAL(2) | CRC(2) | 0D 0A`
- Login `0x01` → extract IMEI (BCD) → ACK
- GPS / GPS+LBS `0x10` / `0x12` / `0x22` …
- Heartbeat / status `0x13`

The parser is intentionally strict on CRC and length so bad frames are dropped early.

## Status

Initial skeleton. Next iterations will flesh out full GPS parsing, robust session handling, metrics, and graceful shutdown.
