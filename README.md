# PTPlus (PicoTun+) v2.5.0

> **High-performance encrypted reverse tunnel** with DPI bypass, gigabit-speed support, TLS transport, and multi-port load balancing.

---

## What's New in PTPlus v2.5.0

### 🚀 Gigabit Speed Support
- **relay buffer**: 32 KB → **256 KB** — eliminates syscall overhead at Gbps speeds  
- **TCP socket buffers**: 64 KB → **4 MB** — removes bandwidth ceiling caused by buffer/RTT limits  
- **smux flow-control buffers**: 1 MB → **4 MB** — prevents smux stall under heavy load

> **Why 64 KB was the bottleneck**: TCP throughput ≈ `buffer / RTT`. At 10 ms RTT, 64 KB caps speed at ~51 Mbps. 4 MB buffers raise the ceiling beyond 1 Gbps.

### 🔒 httpsmux / wssmux (TLS Transport) — Fixed
Server now correctly starts a **TLS listener** when `transport: httpsmux` or `transport: wssmux` is configured. Previously the server used a plain HTTP listener, which could not process the client's TLS handshake.

```yaml
# Server config for httpsmux
transport: httpsmux
cert_file: /etc/picotun/cert.pem
key_file:  /etc/picotun/key.pem
```

Generate a self-signed cert:
```bash
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 3650 -nodes -subj "/CN=yourdomain.com"
```

### 🌐 Multi-Port Load Balancer
Listen on multiple ports simultaneously. Multiple kharej servers connect through different ports on the same Iran server.

```yaml
listen_ports:
  - "0.0.0.0:2020"
  - "0.0.0.0:2021"
  - "0.0.0.0:2022"
```

### 🛡️ DPI Stealth Mode

| Setting | Default | Description |
|---------|---------|-------------|
| `random_padding` | `true` | Variable-length padding per packet |
| `keepalive_jitter` | `2` | Randomize keepalive timing ±2s |
| `conn_jitter_ms` | `500` | Random delay between connections |
| `burst_split` | `false` | Split large writes into random chunks |
| `fake_traffic` | `false` | Periodic fake HTTP data on idle sessions |

### ⚡ High-Capacity (120+ Users)
- Frame size: 2 KB → 4 KB  
- Stream limit: 200 → 512 per session (configurable)  
- Max connections: 300 → 500  
- Keepalive timeout: ×10 → ×15 (less aggressive)

---

## Installation

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ramin-mahmoodi/PTPlus/master/setup.sh)
```

---

## Architecture

```
Users ──→ Iran Server (multi-port) ──smux──→ Kharej Server ──→ Internet
              :2020 ←── kharej-1
              :2021 ←── kharej-2
              :2022 ←── kharej-3
```

---

## Configuration

### Server (Iran) — httpmux (plain HTTP)
```yaml
config_version: 2
mode: "server"
listen: "0.0.0.0:2020"
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

### Server (Iran) — httpsmux (TLS)
```yaml
config_version: 2
mode: "server"
listen: "0.0.0.0:443"
transport: "httpsmux"
cert_file: /etc/picotun/cert.pem
key_file:  /etc/picotun/key.pem
psk: "your-secret-key"
profile: "speed"

maps:
  - { type: tcp, bind: "8443", target: "127.0.0.1:8443" }
```

### Client (Kharej)
```yaml
config_version: 2
mode: "client"
psk: "your-secret-key"
transport: "httpmux"   # or "httpsmux"
profile: "speed"

paths:
  - transport: "httpmux"
    addr: "iran-ip:2020"
    connection_pool: 4
```

---

## Profiles

| Profile | Pool | Keepalive | Use Case |
|---------|------|-----------|----------|
| `speed` | 4 | 2s | Downloads, general |
| `balanced` | 4 | 2s | Mixed usage |
| `gaming` | 6 | 1s | Low latency games |
| `streaming` | 4 | 2s | Video/audio |
| `lowcpu` | 2 | 5s | Low-end servers |

---

## Troubleshooting

### DPI blocks connection
Enable stealth features:
```yaml
stealth:
  random_padding: true
  keepalive_jitter: 3
  conn_jitter_ms: 1000
  burst_split: true
  fake_traffic: true
  fake_traffic_interval: 20
```

### Speed still low
Check your config has the updated defaults (PTPlus auto-migrates old configs). For manual override:
```yaml
smux:
  max_recv: 4194304    # 4MB
  max_stream: 4194304
advanced:
  tcp_read_buffer: 4194304
  tcp_write_buffer: 4194304
```

### httpsmux not connecting
Make sure `cert_file` and `key_file` are set on the **server** side. The client uses `InsecureSkipVerify` so a self-signed cert is fine.

### Gaming micro-disconnects
```yaml
profile: "gaming"
smux:
  keepalive: 1
session_timeout: 60
```

---

## Version History

### PTPlus v2.5.0 *(this release)*
- ✅ **Gigabit speed fix** — relay buffer 32 KB → 256 KB, TCP/smux buffers 64 KB/1 MB → 4 MB
- ✅ **httpsmux TLS fix** — server now runs TLS listener for httpsmux/wssmux transports
- ✅ Multi-port load balancer
- ✅ DPI stealth mode (padding, jitter, fake traffic)
- ✅ Port mapping smux stream-tag fix
- ✅ 120+ user support
- ✅ Config auto-migration
- ✅ Random TLS fingerprint rotation (Chrome / Firefox / Edge / Safari)

### v2.4.0
- Performance profiles
- Multi-IP failover
- TLS fragmentation
- EOF handling fix
