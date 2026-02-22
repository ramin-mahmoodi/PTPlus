# PicoTun v2.5

Encrypted Reverse Tunnel with DPI Bypass, Multi-Port Load Balancing, and High-Capacity Support.

## What's New in v2.5

### Multi-Port Load Balancer
Server can listen on multiple tunnel ports simultaneously. Multiple kharej servers can connect through different ports on the same Iran server.

```yaml
# Server config
listen_ports:
  - "0.0.0.0:2020"
  - "0.0.0.0:2021"
  - "0.0.0.0:2022"
```

### Port Mapping Fix
Fixed smux stream confusion when port mappings are configured. Streams are now tagged with type bytes to prevent misrouting between forward and reverse proxy streams.

### DPI Stealth Mode
New stealth settings that make tunnel traffic harder for DPI to detect:

| Setting | Default | Description |
|---------|---------|-------------|
| `random_padding` | `true` | Variable-length padding per packet |
| `keepalive_jitter` | `2` | Randomize keepalive timing ±2s |
| `conn_jitter_ms` | `500` | Random delay between connections |
| `burst_split` | `true` | Split large writes into random chunks |
| `fake_traffic` | `true` | Periodic fake HTTP data on idle sessions |

```yaml
stealth:
  random_padding: true
  min_padding: 16
  max_padding: 128
  keepalive_jitter: 2
  conn_jitter_ms: 500
  burst_split: true
  max_burst_size: 4096
  fake_traffic: true
  fake_traffic_interval: 30
```

### High-Capacity (120+ Users)
- Smux buffers: 512KB → 1MB
- Frame size: 2KB → 4KB
- TCP buffers: 32KB → 64KB
- Stream limit: 200 → 512 per session (configurable)
- Max connections: 300 → 500
- Keepalive timeout: 10x → 15x (less aggressive)

### Connection Stability
- Graduated reconnection backoff (less micro-disconnects)
- Random reconnection jitter (prevents thundering herd)
- Longer session timeouts (15s → 30s)
- Better zombie session detection
- Random TLS fingerprint rotation (Chrome/Firefox/Edge/Safari)

### Config Auto-Migration
When updating PicoTun, old config files are automatically migrated to v2.5 format:
- Old buffer sizes → new defaults
- Old keepalive values → improved values
- Stealth mode enabled automatically
- Config version tracked

## Installation

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/amir6dev/PicoTun/main/setup.sh)
```

## Architecture

```
Users ──→ Iran Server (multi-port) ──smux──→ Kharej Server ──→ Internet
              :2020 ←── kharej-1
              :2021 ←── kharej-2
              :2022 ←── kharej-3
```

## Configuration

### Server (Iran)
```yaml
config_version: 2
mode: "server"
listen: "0.0.0.0:2020"
listen_ports:
  - "0.0.0.0:2020"
  - "0.0.0.0:2021"
transport: "httpmux"
psk: "your-secret-key"
profile: "speed"

maps:
  - { type: tcp, bind: "443", target: "127.0.0.1:443" }
  - { type: udp, bind: "1234", target: "127.0.0.1:1234" }

stealth:
  random_padding: true
  fake_traffic: true

advanced:
  max_streams_per_session: 512
  max_connections: 500
```

### Client (Kharej)
```yaml
config_version: 2
mode: "client"
psk: "your-secret-key"
transport: "httpmux"
profile: "speed"

paths:
  - transport: "httpmux"
    addr: "iran-ip:2020"
    connection_pool: 4

stealth:
  random_padding: true
  burst_split: true
```

## Profiles

| Profile | Pool | Keepalive | Use Case |
|---------|------|-----------|----------|
| speed | 4 | 2s | Downloads, general |
| balanced | 4 | 2s | Mixed usage |
| gaming | 6 | 1s | Low latency games |
| streaming | 4 | 2s | Video/audio |
| lowcpu | 2 | 5s | Low-end servers |

## Troubleshooting

### DPI blocks IP daily
Enable all stealth features:
```yaml
stealth:
  random_padding: true
  keepalive_jitter: 3
  conn_jitter_ms: 1000
  burst_split: true
  fake_traffic: true
  fake_traffic_interval: 20
```

### Speed drops with many users
Increase capacity settings:
```yaml
smux:
  max_recv: 2097152   # 2MB
  max_stream: 2097152
  frame_size: 8192    # 8KB
advanced:
  max_streams_per_session: 1024
  max_connections: 1000
  tcp_read_buffer: 131072   # 128KB
  tcp_write_buffer: 131072
```

### Gaming micro-disconnects
Use gaming profile and increase keepalive timeout:
```yaml
profile: "gaming"
smux:
  keepalive: 1
session_timeout: 60
```

### Port mapping not working
Make sure the target service is running on the kharej server and accessible locally. Check logs with:
```bash
journalctl -u picotun-server -f
journalctl -u picotun-client -f
```

## Version History

### v2.5.0
- Multi-port load balancer
- Port mapping smux fix  
- DPI stealth mode
- 120+ user support
- Connection stability improvements
- Config auto-migration
- Random TLS fingerprint rotation

### v2.4.0
- Performance profiles
- Multi-IP failover
- EOF handling fix
- TLS fragmentation
