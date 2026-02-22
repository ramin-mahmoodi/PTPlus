#!/bin/bash

# ═══════════════════════════════════════════════════════════════
# PicoTun — Encrypted Reverse Tunnel
# github.com/amir6dev/PicoTun
# ═══════════════════════════════════════════════════════════════

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
WHITE='\033[1;37m'
GRAY='\033[0;90m'
BOLD='\033[1m'
NC='\033[0m'

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/picotun"
SYSTEMD_DIR="/etc/systemd/system"
GITHUB_REPO="amir6dev/PicoTun"
LATEST_RELEASE_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"

# ═════════════════════════════════════════
#  UTILITIES
# ═════════════════════════════════════════

show_banner() {
    clear
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}${BOLD}PicoTun${NC}  ${GRAY}— @amir6dev${NC}"
    echo -e "     ${GRAY}Session Pool | AES-256-GCM | Anti-DPI${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        echo -e "${RED}This script must be run as root${NC}"
        exit 1
    fi
}

install_dependencies() {
    echo -e "${GRAY}Installing dependencies...${NC}"
    if command -v apt &>/dev/null; then
        apt update -qq 2>/dev/null
        apt install -y wget curl tar openssl iproute2 > /dev/null 2>&1
    elif command -v yum &>/dev/null; then
        yum install -y wget curl tar openssl iproute > /dev/null 2>&1
    else
        echo -e "${RED}Unsupported package manager${NC}"
        exit 1
    fi
    echo -e "${GREEN}Dependencies installed${NC}"
}

get_arch() {
    case $(uname -m) in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) echo -e "${RED}Unsupported architecture: $(uname -m)${NC}"; exit 1 ;;
    esac
}

get_current_version() {
    if [ -f "$INSTALL_DIR/picotun" ]; then
        "$INSTALL_DIR/picotun" -version 2>&1 | head -1 || echo "unknown"
    else
        echo "not-installed"
    fi
}

show_service_status() {
    local svc=$1
    local label=$2
    if systemctl is-active "$svc" &>/dev/null; then
        echo -e "  ${GREEN}●${NC} ${label}: ${GREEN}Running${NC}"
    elif systemctl is-enabled "$svc" &>/dev/null 2>&1; then
        echo -e "  ${YELLOW}●${NC} ${label}: ${YELLOW}Stopped${NC}"
    else
        echo -e "  ${GRAY}○${NC} ${label}: ${GRAY}Not installed${NC}"
    fi
}

press_enter() {
    echo ""
    read -p "  Press Enter to continue..."
}

# ═════════════════════════════════════════
#  DOWNLOAD / UPDATE
# ═════════════════════════════════════════

download_binary() {
    echo -e "${YELLOW}Downloading PicoTun binary...${NC}"
    mkdir -p "$INSTALL_DIR"

    local ARCH=$(get_arch)
    local LATEST_VERSION=$(curl -s "$LATEST_RELEASE_API" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$LATEST_VERSION" ]; then
        echo -e "${YELLOW}Could not fetch latest version, using v2.4.0${NC}"
        LATEST_VERSION="v2.4.0"
    fi

    local URL="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/picotun-${LATEST_VERSION}-linux-${ARCH}.tar.gz"
    echo -e "  Version: ${WHITE}${LATEST_VERSION}${NC}  Arch: ${WHITE}${ARCH}${NC}"

    if [ -f "$INSTALL_DIR/picotun" ]; then
        cp "$INSTALL_DIR/picotun" "$INSTALL_DIR/picotun.backup"
    fi

    local TMP=$(mktemp -d)
    if wget -q --show-progress "$URL" -O "$TMP/dl.tar.gz" && tar -xzf "$TMP/dl.tar.gz" -C "$TMP"; then
        mv "$TMP/picotun" "$INSTALL_DIR/picotun"
        chmod +x "$INSTALL_DIR/picotun"
        rm -rf "$TMP" "$INSTALL_DIR/picotun.backup"
        echo -e "${GREEN}Downloaded successfully${NC}"
    else
        rm -rf "$TMP"
        if [ -f "$INSTALL_DIR/picotun.backup" ]; then
            mv "$INSTALL_DIR/picotun.backup" "$INSTALL_DIR/picotun"
            echo -e "${YELLOW}Restored previous version${NC}"
        fi
        echo -e "${RED}Download failed${NC}"
        return 1
    fi
}

# ═════════════════════════════════════════
#  SSL CERTIFICATE
# ═════════════════════════════════════════

generate_ssl_cert() {
    echo ""
    read -p "  Domain for certificate [www.google.com]: " CERT_DOMAIN
    CERT_DOMAIN=${CERT_DOMAIN:-www.google.com}

    mkdir -p "$CONFIG_DIR/certs"
    openssl req -x509 -newkey rsa:4096 \
        -keyout "$CONFIG_DIR/certs/key.pem" \
        -out "$CONFIG_DIR/certs/cert.pem" \
        -days 365 -nodes \
        -subj "/CN=${CERT_DOMAIN}" 2>/dev/null

    CERT_FILE="$CONFIG_DIR/certs/cert.pem"
    KEY_FILE="$CONFIG_DIR/certs/key.pem"
    echo -e "  ${GREEN}Certificate generated (${CERT_DOMAIN})${NC}"
}

# ═════════════════════════════════════════
#  SYSTEMD SERVICE
# ═════════════════════════════════════════

create_systemd_service() {
    local MODE=$1
    cat > "$SYSTEMD_DIR/picotun-${MODE}.service" << SVCEOF
[Unit]
Description=PicoTun ${MODE^} (@amir6dev)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$CONFIG_DIR
ExecStart=$INSTALL_DIR/picotun -c $CONFIG_DIR/${MODE}.yaml
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SVCEOF
    systemctl daemon-reload
}

# ═════════════════════════════════════════
#  SYSTEM OPTIMIZER
# ═════════════════════════════════════════

optimize_system() {
    local ROLE=${1:-""}

    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}SYSTEM OPTIMIZER${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    # BBR
    echo -e "  ${WHITE}Enabling BBR...${NC}"
    modprobe tcp_bbr 2>/dev/null
    if ! grep -q "tcp_bbr" /etc/modules-load.d/modules.conf 2>/dev/null; then
        echo "tcp_bbr" >> /etc/modules-load.d/modules.conf 2>/dev/null
    fi

    cat > /etc/sysctl.d/99-picotun.conf << 'SYSEOF'
# PicoTun Optimizer
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.core.rmem_default=1048576
net.core.wmem_default=1048576
net.core.netdev_max_backlog=5000
net.core.somaxconn=4096
net.ipv4.tcp_rmem=4096 1048576 16777216
net.ipv4.tcp_wmem=4096 1048576 16777216
net.ipv4.tcp_max_syn_backlog=8192
net.ipv4.tcp_slow_start_after_idle=0
net.ipv4.tcp_tw_reuse=1
net.ipv4.ip_local_port_range=1024 65535
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_mtu_probing=1
net.ipv4.tcp_congestion_control=bbr
net.core.default_qdisc=fq
net.ipv4.tcp_notsent_lowat=16384
net.ipv4.tcp_fin_timeout=15
net.ipv4.tcp_keepalive_time=300
net.ipv4.tcp_keepalive_intvl=30
net.ipv4.tcp_keepalive_probes=5
SYSEOF
    sysctl -p /etc/sysctl.d/99-picotun.conf >/dev/null 2>&1

    echo -e "  ${GREEN}BBR enabled${NC}"
    echo -e "  ${GREEN}TCP buffers optimized (16MB max)${NC}"
    echo -e "  ${GREEN}Kernel parameters tuned${NC}"

    # Verify
    local CC=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)
    echo ""
    echo -e "  Congestion Control: ${WHITE}${CC}${NC}"
    echo -e "  ${GREEN}System optimization complete${NC}"
}

system_optimizer_menu() {
    show_banner
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}SYSTEM OPTIMIZER${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
    echo "  1) Apply Full Optimization (BBR + Buffers)"
    echo "  2) Check Current Settings"
    echo "  3) Remove Optimizations"
    echo ""
    echo "  0) Back to Main Menu"
    echo ""
    read -p "  Select: " choice

    case $choice in
        1)
            optimize_system
            press_enter
            system_optimizer_menu
            ;;
        2)
            echo ""
            echo -e "  Congestion: ${WHITE}$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)${NC}"
            echo -e "  Qdisc:      ${WHITE}$(sysctl -n net.core.default_qdisc 2>/dev/null)${NC}"
            echo -e "  Rmem Max:   ${WHITE}$(sysctl -n net.core.rmem_max 2>/dev/null)${NC}"
            echo -e "  Wmem Max:   ${WHITE}$(sysctl -n net.core.wmem_max 2>/dev/null)${NC}"
            press_enter
            system_optimizer_menu
            ;;
        3)
            rm -f /etc/sysctl.d/99-picotun.conf
            sysctl -p >/dev/null 2>&1
            echo -e "  ${GREEN}Optimizations removed${NC}"
            press_enter
            system_optimizer_menu
            ;;
        0) main_menu ;;
        *) system_optimizer_menu ;;
    esac
}

# ═════════════════════════════════════════
#  PORT MAPPING (shared function)
# ═════════════════════════════════════════

port_mapping_wizard() {
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}PORT MAPPINGS${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${WHITE}Help:${NC}"
    echo -e "  ${GRAY}Single Port:     ${WHITE}8008${GRAY}            → 8008 to 8008${NC}"
    echo -e "  ${GRAY}Range:           ${WHITE}1000/2000${GRAY}       → 1000-2000 same ports${NC}"
    echo -e "  ${GRAY}Custom Mapping:  ${WHITE}5000=8008${GRAY}       → 5000 to 8008${NC}"
    echo -e "  ${GRAY}Range Mapping:   ${WHITE}1000/1010=2000/2010${GRAY} → mapped range${NC}"
    echo ""

    MAPPINGS=""
    MAP_COUNT=0

    while true; do
        echo -e "  ${CYAN}--- Port Mapping #$((MAP_COUNT+1)) ---${NC}"
        echo ""
        echo "  Protocol:"
        echo "    1) tcp"
        echo "    2) udp"
        echo "    3) both (tcp + udp)"
        echo ""
        read -p "    Choice [1-3]: " proto_choice

        case $proto_choice in
            1) PROTO="tcp" ;;
            2) PROTO="udp" ;;
            3) PROTO="both" ;;
            *) PROTO="tcp" ;;
        esac

        echo ""
        read -p "    Enter port(s): " PORT_INPUT

        if [ -z "$PORT_INPUT" ]; then
            echo -e "    ${RED}Port cannot be empty${NC}"
            continue
        fi

        PORT_INPUT=$(echo "$PORT_INPUT" | tr -d ' ')
        BIND_IP="0.0.0.0"
        TARGET_IP="127.0.0.1"

        # Range Mapping (1000/1010=2000/2010)
        if [[ "$PORT_INPUT" =~ ^([0-9]+)/([0-9]+)=([0-9]+)/([0-9]+)$ ]]; then
            local BS=${BASH_REMATCH[1]} BE=${BASH_REMATCH[2]}
            local TS=${BASH_REMATCH[3]} TE=${BASH_REMATCH[4]}
            local BRANGE=$((BE - BS + 1))
            local TRANGE=$((TE - TS + 1))

            if [ "$BRANGE" -ne "$TRANGE" ]; then
                echo -e "    ${RED}Range size mismatch${NC}"
                continue
            fi

            if [ "$BS" -lt 1 ] || [ "$BE" -gt 65535 ] || [ "$TS" -lt 1 ] || [ "$TE" -gt 65535 ]; then
                echo -e "    ${RED}Invalid port range (1-65535)${NC}"
                continue
            fi

            for ((i=0; i<BRANGE; i++)); do
                local BP=$((BS + i))
                local TP=$((TS + i))
                add_mapping "$PROTO" "${BIND_IP}:${BP}" "${TARGET_IP}:${TP}"
            done
            echo -e "    ${GREEN}Added $BRANGE mappings: ${BS}-${BE} → ${TS}-${TE} (${PROTO})${NC}"

        # Range (1000/2000)
        elif [[ "$PORT_INPUT" =~ ^([0-9]+)/([0-9]+)$ ]]; then
            local SP=${BASH_REMATCH[1]} EP=${BASH_REMATCH[2]}

            if [ "$SP" -gt "$EP" ]; then
                echo -e "    ${RED}Start port must be less than end port${NC}"
                continue
            fi

            if [ "$SP" -lt 1 ] || [ "$EP" -gt 65535 ]; then
                echo -e "    ${RED}Invalid port range (1-65535)${NC}"
                continue
            fi

            local RSIZE=$((EP - SP + 1))
            if [ "$RSIZE" -gt 1000 ]; then
                echo -e "    ${YELLOW}Large range: ${RSIZE} ports${NC}"
                read -p "    Continue? [y/N]: " confirm
                [[ ! $confirm =~ ^[Yy]$ ]] && continue
            fi

            for ((port=SP; port<=EP; port++)); do
                add_mapping "$PROTO" "${BIND_IP}:${port}" "${TARGET_IP}:${port}"
            done
            echo -e "    ${GREEN}Added $RSIZE mappings: ${SP}-${EP} (${PROTO})${NC}"

        # Custom Mapping (5000=8008)
        elif [[ "$PORT_INPUT" =~ ^([0-9]+)=([0-9]+)$ ]]; then
            local BP=${BASH_REMATCH[1]} TP=${BASH_REMATCH[2]}

            if [ "$BP" -lt 1 ] || [ "$BP" -gt 65535 ] || [ "$TP" -lt 1 ] || [ "$TP" -gt 65535 ]; then
                echo -e "    ${RED}Invalid port (1-65535)${NC}"
                continue
            fi

            add_mapping "$PROTO" "${BIND_IP}:${BP}" "${TARGET_IP}:${TP}"
            echo -e "    ${GREEN}Mapping: ${BP} → ${TP} (${PROTO})${NC}"

        # Single Port (8008)
        elif [[ "$PORT_INPUT" =~ ^[0-9]+$ ]]; then
            if [ "$PORT_INPUT" -lt 1 ] || [ "$PORT_INPUT" -gt 65535 ]; then
                echo -e "    ${RED}Invalid port (1-65535)${NC}"
                continue
            fi

            add_mapping "$PROTO" "${BIND_IP}:${PORT_INPUT}" "${TARGET_IP}:${PORT_INPUT}"
            echo -e "    ${GREEN}Mapping: ${PORT_INPUT} (${PROTO})${NC}"

        else
            echo -e "    ${RED}Invalid format${NC}"
            continue
        fi

        echo ""
        read -p "    Add another mapping? [y/N]: " add_more
        [[ ! "$add_more" =~ ^[Yy]$ ]] && break
    done

    if [ "$MAP_COUNT" -eq 0 ]; then
        echo -e "    ${YELLOW}No mappings added. Adding default 8080...${NC}"
        add_mapping "tcp" "0.0.0.0:8080" "127.0.0.1:8080"
    fi
}

add_mapping() {
    local proto=$1 bind=$2 target=$3
    if [ "$proto" = "both" ]; then
        MAPPINGS="${MAPPINGS}  - type: tcp\n    bind: \"${bind}\"\n    target: \"${target}\"\n"
        MAPPINGS="${MAPPINGS}  - type: udp\n    bind: \"${bind}\"\n    target: \"${target}\"\n"
        MAP_COUNT=$((MAP_COUNT + 2))
    else
        MAPPINGS="${MAPPINGS}  - type: ${proto}\n    bind: \"${bind}\"\n    target: \"${target}\"\n"
        MAP_COUNT=$((MAP_COUNT + 1))
    fi
}

# ═════════════════════════════════════════
#  HTTP MIMICRY CONFIG
# ═════════════════════════════════════════

configure_http_mimicry() {
    echo ""
    echo -e "  ${WHITE}HTTP Mimicry Configuration:${NC}"
    echo ""
    read -p "    Fake Domain [www.google.com]: " HTTP_DOMAIN
    HTTP_DOMAIN=${HTTP_DOMAIN:-www.google.com}

    read -p "    Fake Path [/search]: " HTTP_PATH
    HTTP_PATH=${HTTP_PATH:-/search}

    read -p "    User-Agent [Chrome]: " HTTP_UA
    HTTP_UA=${HTTP_UA:-Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36}

    echo -e "    ${GREEN}HTTP Mimicry: ${HTTP_DOMAIN}${HTTP_PATH}${NC}"
}

# ═════════════════════════════════════════
#  ADVANCED SETTINGS (smux/tcp/obfs)
# ═════════════════════════════════════════

configure_advanced_settings() {
    local MODE=$1
    echo ""
    echo -e "  ${WHITE}Advanced Settings:${NC}"
    echo ""
    echo "    1) Use optimized defaults (Recommended)"
    echo "    2) Custom configuration"
    echo ""
    read -p "    Choice [1-2]: " adv_choice

    if [ "$adv_choice" != "2" ]; then
        # Optimized defaults
        SMUX_KEEPALIVE=1
        SMUX_MAXRECV=524288
        SMUX_MAXSTREAM=524288
        SMUX_FRAMESIZE=2048
        TCP_NODELAY="true"
        TCP_KEEPALIVE=3
        TCP_READBUFFER=32768
        TCP_WRITEBUFFER=32768
        MAX_CONNECTIONS=300
        echo -e "    ${GREEN}Using optimized defaults${NC}"
        return
    fi

    echo ""
    echo -e "  ${WHITE}SMUX Settings:${NC}"
    read -p "    KeepAlive interval (seconds) [10]: " SMUX_KEEPALIVE
    SMUX_KEEPALIVE=${SMUX_KEEPALIVE:-1}
    read -p "    Max receive buffer [4194304]: " SMUX_MAXRECV
    SMUX_MAXRECV=${SMUX_MAXRECV:-524288}
    read -p "    Max stream buffer [4194304]: " SMUX_MAXSTREAM
    SMUX_MAXSTREAM=${SMUX_MAXSTREAM:-524288}
    read -p "    Frame size [32768]: " SMUX_FRAMESIZE
    SMUX_FRAMESIZE=${SMUX_FRAMESIZE:-2048}

    echo ""
    echo -e "  ${WHITE}TCP Settings:${NC}"
    read -p "    TCP NoDelay [true]: " TCP_NODELAY
    TCP_NODELAY=${TCP_NODELAY:-true}
    read -p "    TCP KeepAlive (seconds) [10]: " TCP_KEEPALIVE
    TCP_KEEPALIVE=${TCP_KEEPALIVE:-3}
    read -p "    Read buffer [32768]: " TCP_READBUFFER
    TCP_READBUFFER=${TCP_READBUFFER:-32768}
    read -p "    Write buffer [32768]: " TCP_WRITEBUFFER
    TCP_WRITEBUFFER=${TCP_WRITEBUFFER:-32768}
    read -p "    Max connections [1000]: " MAX_CONNECTIONS
    MAX_CONNECTIONS=${MAX_CONNECTIONS:-300}
}

# ═════════════════════════════════════════
#  OBFUSCATION CONFIG
# ═════════════════════════════════════════

configure_obfuscation() {
    echo ""
    read -p "  Enable Traffic Obfuscation? [Y/n]: " obf_en

    if [[ $obf_en =~ ^[Nn]$ ]]; then
        OBFS_ENABLED="false"
        OBFS_MIN_PAD=4; OBFS_MAX_PAD=32
        OBFS_MIN_DELAY=0; OBFS_MAX_DELAY=0
        return
    fi

    OBFS_ENABLED="true"
    echo ""
    read -p "  Configure obfuscation details? [y/N]: " obf_detail
    if [[ $obf_detail =~ ^[Yy]$ ]]; then
        read -p "    Min padding (bytes) [4]: " OBFS_MIN_PAD
        OBFS_MIN_PAD=${OBFS_MIN_PAD:-4}
        read -p "    Max padding (bytes) [32]: " OBFS_MAX_PAD
        OBFS_MAX_PAD=${OBFS_MAX_PAD:-32}
        read -p "    Min delay (ms) [0]: " OBFS_MIN_DELAY
        OBFS_MIN_DELAY=${OBFS_MIN_DELAY:-0}
        read -p "    Max delay (ms) [0]: " OBFS_MAX_DELAY
        OBFS_MAX_DELAY=${OBFS_MAX_DELAY:-0}
    else
        OBFS_MIN_PAD=4; OBFS_MAX_PAD=32
        OBFS_MIN_DELAY=0; OBFS_MAX_DELAY=0
    fi
}

# ═════════════════════════════════════════
#  WRITE CONFIG FILE
# ═════════════════════════════════════════

write_server_config() {
    local CONFIG_FILE="$CONFIG_DIR/server.yaml"
    cat > "$CONFIG_FILE" << EOF
config_version: 2
mode: "server"
listen: "0.0.0.0:${LISTEN_PORT}"
transport: "${TRANSPORT}"
psk: "${PSK}"
profile: "${PROFILE}"
verbose: ${VERBOSE}
heartbeat: 5
num_connections: 4
EOF

    # v2.5: Multi-port listen
    if [ -n "$EXTRA_PORTS" ]; then
        cat >> "$CONFIG_FILE" << EOF

listen_ports:
  - "0.0.0.0:${LISTEN_PORT}"
$(echo -e "$EXTRA_PORTS")
EOF
    fi

    if [ -n "$CERT_FILE" ]; then
        cat >> "$CONFIG_FILE" << EOF

cert_file: "$CERT_FILE"
key_file: "$KEY_FILE"
EOF
    fi

    printf "\nmaps:\n$MAPPINGS" >> "$CONFIG_FILE"

    cat >> "$CONFIG_FILE" << EOF

smux:
  keepalive: ${SMUX_KEEPALIVE}
  max_recv: ${SMUX_MAXRECV}
  max_stream: ${SMUX_MAXSTREAM}
  frame_size: ${SMUX_FRAMESIZE}
  version: 2

fragment:
  enabled: true
  min_size: 64
  max_size: 191
  min_delay: 1
  max_delay: 3

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

advanced:
  tcp_nodelay: ${TCP_NODELAY}
  tcp_keepalive: ${TCP_KEEPALIVE}
  tcp_read_buffer: ${TCP_READBUFFER}
  tcp_write_buffer: ${TCP_WRITEBUFFER}
  max_connections: ${MAX_CONNECTIONS}
  max_streams_per_session: ${MAX_STREAMS:-512}
  cleanup_interval: 3
  connection_timeout: 30
  stream_timeout: 60
  max_udp_flows: 1000
  udp_flow_timeout: 300
  udp_buffer_size: 4194304

obfuscation:
  enabled: ${OBFS_ENABLED}
  min_padding: ${OBFS_MIN_PAD}
  max_padding: ${OBFS_MAX_PAD}
  min_delay_ms: ${OBFS_MIN_DELAY}
  max_delay_ms: ${OBFS_MAX_DELAY}
EOF

    if [ "$TRANSPORT" == "httpmux" ] || [ "$TRANSPORT" == "httpsmux" ]; then
        cat >> "$CONFIG_FILE" << EOF

http_mimic:
  fake_domain: "${HTTP_DOMAIN}"
  fake_path: "${HTTP_PATH}"
  user_agent: "${HTTP_UA}"
  session_cookie: true
  custom_headers:
    - "Accept-Language: en-US,en;q=0.9"
    - "Accept-Encoding: gzip, deflate, br"
    - "Referer: https://${HTTP_DOMAIN}/"
EOF
    fi
}

write_client_config() {
    local CONFIG_FILE="$CONFIG_DIR/client.yaml"
    cat > "$CONFIG_FILE" << EOF
config_version: 2
mode: "client"
psk: "${PSK}"
transport: "${TRANSPORT}"
profile: "${PROFILE}"
verbose: ${VERBOSE}
heartbeat: 5
num_connections: ${POOL_SIZE}

paths:
  - transport: "${TRANSPORT}"
    addr: "${SERVER_ADDR}"
    connection_pool: ${POOL_SIZE}
    retry_interval: 3
    dial_timeout: 15

smux:
  keepalive: ${SMUX_KEEPALIVE}
  max_recv: ${SMUX_MAXRECV}
  max_stream: ${SMUX_MAXSTREAM}
  frame_size: ${SMUX_FRAMESIZE}
  version: 2

fragment:
  enabled: true
  min_size: 64
  max_size: 191
  min_delay: 1
  max_delay: 3

stealth:
  random_padding: true
  min_padding: 16
  max_padding: 128
  keepalive_jitter: 2
  conn_jitter_ms: 500
  burst_split: true
  max_burst_size: 4096

advanced:
  tcp_nodelay: ${TCP_NODELAY}
  tcp_keepalive: ${TCP_KEEPALIVE}
  tcp_read_buffer: ${TCP_READBUFFER}
  tcp_write_buffer: ${TCP_WRITEBUFFER}
  connection_timeout: 30

obfuscation:
  enabled: ${OBFS_ENABLED}
  min_padding: ${OBFS_MIN_PAD}
  max_padding: ${OBFS_MAX_PAD}
  min_delay_ms: ${OBFS_MIN_DELAY}
  max_delay_ms: ${OBFS_MAX_DELAY}
EOF

    if [ "$TRANSPORT" == "httpmux" ] || [ "$TRANSPORT" == "httpsmux" ]; then
        cat >> "$CONFIG_FILE" << EOF

http_mimic:
  fake_domain: "${HTTP_DOMAIN}"
  fake_path: "${HTTP_PATH}"
  user_agent: "${HTTP_UA}"
  session_cookie: true
  custom_headers:
    - "Accept-Language: en-US,en;q=0.9"
    - "Accept-Encoding: gzip, deflate, br"
    - "Referer: https://${HTTP_DOMAIN}/"
EOF
    fi
}

# ═════════════════════════════════════════
#  INSTALL SERVER (Automatic)
# ═════════════════════════════════════════

install_server_automatic() {
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}AUTOMATIC SERVER CONFIGURATION${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    read -p "  Tunnel Port [2020]: " LISTEN_PORT
    LISTEN_PORT=${LISTEN_PORT:-2020}

    echo ""
    while true; do
        read -sp "  PSK (Pre-Shared Key): " PSK
        echo ""
        if [ -n "$PSK" ]; then break; fi
        echo -e "  ${RED}PSK cannot be empty${NC}"
    done

    echo ""
    echo -e "  ${WHITE}Select Transport:${NC}"
    echo "    1) httpmux   - HTTP Mimicry (DPI bypass)"
    echo "    2) httpsmux  - HTTPS Mimicry (TLS + DPI bypass)"
    echo "    3) tcpmux    - Simple TCP"
    echo ""
    read -p "  Choice [1-3]: " trans_choice
    case $trans_choice in
        1) TRANSPORT="httpmux" ;;
        2) TRANSPORT="httpsmux" ;;
        3) TRANSPORT="tcpmux" ;;
        *) TRANSPORT="httpmux" ;;
    esac

    # Port mappings
    port_mapping_wizard

    # SSL cert
    CERT_FILE=""
    KEY_FILE=""
    if [ "$TRANSPORT" == "httpsmux" ]; then
        generate_ssl_cert
    fi

    # HTTP Mimicry defaults
    HTTP_DOMAIN="www.google.com"
    HTTP_PATH="/search"
    HTTP_UA="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

    # v2.5: Multi-port tunnel (optional)
    echo ""
    echo -e "  ${WHITE}Multi-Port Tunnel (Load Balancer):${NC}"
    echo -e "  ${GRAY}Add extra tunnel ports so multiple kharej servers${NC}"
    echo -e "  ${GRAY}can connect on different ports. Leave empty to skip.${NC}"
    echo ""
    EXTRA_PORTS=""
    while true; do
        read -p "  Add extra tunnel port (or Enter to skip): " extra_port
        if [ -z "$extra_port" ]; then break; fi
        EXTRA_PORTS="${EXTRA_PORTS}  - \"0.0.0.0:${extra_port}\"\n"
        echo -e "  ${GREEN}Added port ${extra_port}${NC}"
    done

    # v2.5 optimized defaults
    PROFILE="speed"
    VERBOSE="true"
    SMUX_KEEPALIVE=2; SMUX_MAXRECV=1048576; SMUX_MAXSTREAM=1048576; SMUX_FRAMESIZE=4096
    TCP_NODELAY="true"; TCP_KEEPALIVE=5; TCP_READBUFFER=65536; TCP_WRITEBUFFER=65536
    MAX_CONNECTIONS=500; MAX_STREAMS=512
    OBFS_ENABLED="true"; OBFS_MIN_PAD=16; OBFS_MAX_PAD=64; OBFS_MIN_DELAY=0; OBFS_MAX_DELAY=0

    # Write config
    mkdir -p "$CONFIG_DIR"
    write_server_config
    create_systemd_service "server"

    echo ""
    read -p "  Optimize system? [Y/n]: " opt
    if [[ ! $opt =~ ^[Nn]$ ]]; then
        optimize_system
    fi

    systemctl start picotun-server
    systemctl enable picotun-server 2>/dev/null

    echo ""
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo -e "     ${GREEN}Server installed successfully${NC}"
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  Tunnel Port: ${WHITE}${LISTEN_PORT}${NC}"
    echo -e "  PSK:         ${WHITE}${PSK}${NC}"
    echo -e "  Transport:   ${WHITE}${TRANSPORT}${NC}"
    echo -e "  Profile:     ${WHITE}${PROFILE}${NC}"
    echo -e "  Config:      ${GRAY}${CONFIG_DIR}/server.yaml${NC}"
    echo -e "  Logs:        ${GRAY}journalctl -u picotun-server -f${NC}"

    press_enter
    main_menu
}

# ═════════════════════════════════════════
#  INSTALL SERVER (Manual)
# ═════════════════════════════════════════

install_server_manual() {
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}MANUAL SERVER CONFIGURATION${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    echo -e "  ${WHITE}Select Transport:${NC}"
    echo "    1) tcpmux    - TCP Multiplexing (Simple)"
    echo "    2) httpmux   - HTTP Mimicry (DPI bypass)"
    echo "    3) httpsmux  - HTTPS Mimicry (TLS + DPI bypass)"
    echo ""
    read -p "  Choice [1-3]: " trans_choice
    case $trans_choice in
        1) TRANSPORT="tcpmux" ;;
        2) TRANSPORT="httpmux" ;;
        3) TRANSPORT="httpsmux" ;;
        *) TRANSPORT="httpmux" ;;
    esac

    echo ""
    read -p "  Tunnel Port [2020]: " LISTEN_PORT
    LISTEN_PORT=${LISTEN_PORT:-2020}

    echo ""
    while true; do
        read -sp "  PSK (Pre-Shared Key): " PSK
        echo ""
        if [ -n "$PSK" ]; then break; fi
        echo -e "  ${RED}PSK cannot be empty${NC}"
    done

    echo ""
    echo -e "  ${WHITE}Performance Profile:${NC}"
    echo "    1) speed       - Max throughput (Recommended)"
    echo "    2) balanced    - General purpose"
    echo "    3) gaming      - Ultra-low latency"
    echo "    4) streaming   - Video/audio optimized"
    echo "    5) lowcpu      - Minimal resources"
    echo ""
    read -p "  Choice [1-5]: " prof_choice
    case $prof_choice in
        1) PROFILE="speed" ;;
        2) PROFILE="balanced" ;;
        3) PROFILE="gaming" ;;
        4) PROFILE="streaming" ;;
        5) PROFILE="lowcpu" ;;
        *) PROFILE="speed" ;;
    esac

    # SSL
    CERT_FILE=""
    KEY_FILE=""
    if [ "$TRANSPORT" == "httpsmux" ]; then
        echo ""
        echo -e "  ${WHITE}TLS Certificate:${NC}"
        echo "    1) Generate self-signed (Quick)"
        echo "    2) Use existing files"
        echo ""
        read -p "  Choice [1-2]: " cert_choice
        if [ "$cert_choice" == "2" ]; then
            read -p "    Certificate path: " CERT_FILE
            read -p "    Private key path: " KEY_FILE
            if [ ! -f "$CERT_FILE" ] || [ ! -f "$KEY_FILE" ]; then
                echo -e "    ${YELLOW}Files not found, generating self-signed...${NC}"
                generate_ssl_cert
            fi
        else
            generate_ssl_cert
        fi
    fi

    # HTTP Mimicry
    HTTP_DOMAIN="www.google.com"
    HTTP_PATH="/search"
    HTTP_UA="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
    if [ "$TRANSPORT" == "httpmux" ] || [ "$TRANSPORT" == "httpsmux" ]; then
        configure_http_mimicry
    fi

    # Obfuscation
    configure_obfuscation

    # Advanced
    configure_advanced_settings "server"

    # Verbose
    echo ""
    read -p "  Enable verbose logging? [Y/n]: " verb
    [[ $verb =~ ^[Nn]$ ]] && VERBOSE="false" || VERBOSE="true"

    # Port mappings
    port_mapping_wizard

    # Write
    mkdir -p "$CONFIG_DIR"
    write_server_config
    create_systemd_service "server"

    echo ""
    read -p "  Optimize system? [Y/n]: " opt
    [[ ! $opt =~ ^[Nn]$ ]] && optimize_system

    systemctl start picotun-server
    systemctl enable picotun-server 2>/dev/null

    echo ""
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo -e "     ${GREEN}Server installed successfully${NC}"
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  Tunnel Port: ${WHITE}${LISTEN_PORT}${NC}"
    echo -e "  PSK:         ${WHITE}${PSK}${NC}"
    echo -e "  Transport:   ${WHITE}${TRANSPORT}${NC}"
    echo -e "  Profile:     ${WHITE}${PROFILE}${NC}"
    if [ "$TRANSPORT" == "httpmux" ] || [ "$TRANSPORT" == "httpsmux" ]; then
        echo -e "  Mimicry:     ${WHITE}${HTTP_DOMAIN}${NC}"
    fi
    echo -e "  Config:      ${GRAY}${CONFIG_DIR}/server.yaml${NC}"
    echo -e "  Logs:        ${GRAY}journalctl -u picotun-server -f${NC}"

    press_enter
    main_menu
}

# ═════════════════════════════════════════
#  INSTALL SERVER (menu)
# ═════════════════════════════════════════

install_server() {
    show_banner
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}SERVER CONFIGURATION${NC}  ${GRAY}(Iran)${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${WHITE}Configuration Mode:${NC}"
    echo "    1) Automatic - Optimized settings (Recommended)"
    echo "    2) Manual    - Custom configuration"
    echo ""
    echo "    0) Back"
    echo ""
    read -p "  Choice: " mode

    case $mode in
        1) install_server_automatic ;;
        2) install_server_manual ;;
        0) main_menu ;;
        *) install_server ;;
    esac
}

# ═════════════════════════════════════════
#  INSTALL CLIENT (Automatic)
# ═════════════════════════════════════════

install_client_automatic() {
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}AUTOMATIC CLIENT CONFIGURATION${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    while true; do
        read -sp "  PSK (must match server): " PSK
        echo ""
        if [ -n "$PSK" ]; then break; fi
        echo -e "  ${RED}PSK cannot be empty${NC}"
    done

    echo ""
    echo -e "  ${WHITE}Select Transport (must match server):${NC}"
    echo "    1) httpmux   - HTTP Mimicry"
    echo "    2) httpsmux  - HTTPS Mimicry"
    echo "    3) tcpmux    - Simple TCP"
    echo ""
    read -p "  Choice [1-3]: " trans_choice
    case $trans_choice in
        1) TRANSPORT="httpmux" ;;
        2) TRANSPORT="httpsmux" ;;
        3) TRANSPORT="tcpmux" ;;
        *) TRANSPORT="httpmux" ;;
    esac

    echo ""
    read -p "  Server IP:Port (e.g., 1.2.3.4:2020): " SERVER_ADDR
    if [ -z "$SERVER_ADDR" ]; then
        echo -e "  ${RED}Address required${NC}"
        press_enter
        main_menu
        return
    fi

    echo ""
    echo -e "  ${WHITE}Connection Pool Size:${NC}"
    echo -e "  ${GRAY}More connections = better speed & reliability${NC}"
    echo "    1) 4 connections (Recommended)"
    echo "    2) 6 connections (High traffic)"
    echo "    3) 8 connections (Heavy load)"
    echo ""
    read -p "  Choice [1-3]: " pool_choice
    case $pool_choice in
        2) POOL_SIZE=6 ;;
        3) POOL_SIZE=8 ;;
        *) POOL_SIZE=4 ;;
    esac

    # v2.5 defaults
    PROFILE="speed"
    VERBOSE="true"
    SMUX_KEEPALIVE=2; SMUX_MAXRECV=1048576; SMUX_MAXSTREAM=1048576; SMUX_FRAMESIZE=4096
    TCP_NODELAY="true"; TCP_KEEPALIVE=5; TCP_READBUFFER=65536; TCP_WRITEBUFFER=65536
    OBFS_ENABLED="true"; OBFS_MIN_PAD=16; OBFS_MAX_PAD=64; OBFS_MIN_DELAY=0; OBFS_MAX_DELAY=0
    HTTP_DOMAIN="www.google.com"
    HTTP_PATH="/search"
    HTTP_UA="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

    mkdir -p "$CONFIG_DIR"
    write_client_config

    # Ask about backup IP
    echo ""
    echo -e "  ${WHITE}Backup IP (Anti-Block):${NC}"
    echo -e "  ${GRAY}If main IP is blocked, PicoTun auto-switches to backup.${NC}"
    echo ""
    read -p "  Add backup server IP:Port? (empty=skip): " BACKUP_ADDR
    if [ -n "$BACKUP_ADDR" ]; then
        cat >> "$CONFIG_DIR/client.yaml" << EOF
  - transport: "${TRANSPORT}"
    addr: "${BACKUP_ADDR}"
    connection_pool: ${POOL_SIZE}
    retry_interval: 3
    dial_timeout: 15
EOF
        echo -e "    ${GREEN}Backup path added: ${BACKUP_ADDR}${NC}"

        # Allow a second backup
        read -p "  Add another backup IP:Port? (empty=skip): " BACKUP_ADDR2
        if [ -n "$BACKUP_ADDR2" ]; then
            cat >> "$CONFIG_DIR/client.yaml" << EOF
  - transport: "${TRANSPORT}"
    addr: "${BACKUP_ADDR2}"
    connection_pool: ${POOL_SIZE}
    retry_interval: 3
    dial_timeout: 15
EOF
            echo -e "    ${GREEN}Backup path added: ${BACKUP_ADDR2}${NC}"
        fi
    fi

    create_systemd_service "client"

    echo ""
    read -p "  Optimize system? [Y/n]: " opt
    [[ ! $opt =~ ^[Nn]$ ]] && optimize_system

    systemctl start picotun-client
    systemctl enable picotun-client 2>/dev/null

    echo ""
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo -e "     ${GREEN}Client installed successfully${NC}"
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  Server:    ${WHITE}${SERVER_ADDR}${NC}"
    echo -e "  PSK:       ${WHITE}${PSK}${NC}"
    echo -e "  Transport: ${WHITE}${TRANSPORT}${NC}"
    echo -e "  Pool:      ${WHITE}${POOL_SIZE} connections${NC}"
    echo -e "  Config:    ${GRAY}${CONFIG_DIR}/client.yaml${NC}"
    echo -e "  Logs:      ${GRAY}journalctl -u picotun-client -f${NC}"

    press_enter
    main_menu
}

# ═════════════════════════════════════════
#  INSTALL CLIENT (Manual)
# ═════════════════════════════════════════

install_client_manual() {
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}MANUAL CLIENT CONFIGURATION${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    while true; do
        read -sp "  PSK (must match server): " PSK
        echo ""
        if [ -n "$PSK" ]; then break; fi
        echo -e "  ${RED}PSK cannot be empty${NC}"
    done

    echo ""
    echo -e "  ${WHITE}Select Transport (must match server):${NC}"
    echo "    1) tcpmux    - TCP Multiplexing"
    echo "    2) httpmux   - HTTP Mimicry"
    echo "    3) httpsmux  - HTTPS Mimicry"
    echo ""
    read -p "  Choice [1-3]: " trans_choice
    case $trans_choice in
        1) TRANSPORT="tcpmux" ;;
        2) TRANSPORT="httpmux" ;;
        3) TRANSPORT="httpsmux" ;;
        *) TRANSPORT="httpmux" ;;
    esac

    echo ""
    read -p "  Server IP:Port (e.g., 1.2.3.4:2020): " SERVER_ADDR
    if [ -z "$SERVER_ADDR" ]; then
        echo -e "  ${RED}Address required${NC}"
        press_enter
        main_menu
        return
    fi

    echo ""
    echo -e "  ${WHITE}Connection Pool Size:${NC}"
    echo "    1) 2 connections"
    echo "    2) 4 connections (Recommended)"
    echo "    3) 6 connections"
    echo "    4) 8 connections"
    echo ""
    read -p "  Choice [1-4]: " pool_choice
    case $pool_choice in
        1) POOL_SIZE=2 ;;
        3) POOL_SIZE=6 ;;
        4) POOL_SIZE=8 ;;
        *) POOL_SIZE=4 ;;
    esac

    echo ""
    echo -e "  ${WHITE}Performance Profile:${NC}"
    echo "    1) speed       - Max throughput (Recommended)"
    echo "    2) balanced    - General purpose"
    echo "    3) gaming      - Ultra-low latency"
    echo "    4) streaming   - Video/audio optimized"
    echo "    5) lowcpu      - Minimal resources"
    echo ""
    read -p "  Choice [1-5]: " prof_choice
    case $prof_choice in
        1) PROFILE="speed" ;;
        2) PROFILE="balanced" ;;
        3) PROFILE="gaming" ;;
        4) PROFILE="streaming" ;;
        5) PROFILE="lowcpu" ;;
        *) PROFILE="speed" ;;
    esac

    # HTTP Mimicry
    HTTP_DOMAIN="www.google.com"
    HTTP_PATH="/search"
    HTTP_UA="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
    if [ "$TRANSPORT" == "httpmux" ] || [ "$TRANSPORT" == "httpsmux" ]; then
        configure_http_mimicry
    fi

    configure_obfuscation
    configure_advanced_settings "client"

    echo ""
    read -p "  Enable verbose logging? [Y/n]: " verb
    [[ $verb =~ ^[Nn]$ ]] && VERBOSE="false" || VERBOSE="true"

    mkdir -p "$CONFIG_DIR"
    write_client_config
    create_systemd_service "client"

    echo ""
    read -p "  Optimize system? [Y/n]: " opt
    [[ ! $opt =~ ^[Nn]$ ]] && optimize_system

    systemctl start picotun-client
    systemctl enable picotun-client 2>/dev/null

    echo ""
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo -e "     ${GREEN}Client installed successfully${NC}"
    echo -e "${GREEN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  Server:    ${WHITE}${SERVER_ADDR}${NC}"
    echo -e "  PSK:       ${WHITE}${PSK}${NC}"
    echo -e "  Transport: ${WHITE}${TRANSPORT}${NC}"
    echo -e "  Pool:      ${WHITE}${POOL_SIZE} connections${NC}"
    echo -e "  Config:    ${GRAY}${CONFIG_DIR}/client.yaml${NC}"
    echo -e "  Logs:      ${GRAY}journalctl -u picotun-client -f${NC}"

    press_enter
    main_menu
}

# ═════════════════════════════════════════
#  INSTALL CLIENT (menu)
# ═════════════════════════════════════════

install_client() {
    show_banner
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}CLIENT CONFIGURATION${NC}  ${GRAY}(Kharej)${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${WHITE}Configuration Mode:${NC}"
    echo "    1) Automatic - Optimized settings (Recommended)"
    echo "    2) Manual    - Custom configuration"
    echo ""
    echo "    0) Back"
    echo ""
    read -p "  Choice: " mode

    case $mode in
        1) install_client_automatic ;;
        2) install_client_manual ;;
        0) main_menu ;;
        *) install_client ;;
    esac
}

# ═════════════════════════════════════════
#  ADD PORTS TO EXISTING SERVER
# ═════════════════════════════════════════

add_ports_to_server() {
    local CONFIG_FILE="$CONFIG_DIR/server.yaml"

    if [ ! -f "$CONFIG_FILE" ]; then
        echo -e "  ${RED}Server config not found${NC}"
        press_enter
        return
    fi

    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}ADD PORT MAPPINGS${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    # Show current mappings
    echo -e "  ${WHITE}Current mappings:${NC}"
    grep -A2 "type:" "$CONFIG_FILE" | while IFS= read -r line; do
        if echo "$line" | grep -q "type:"; then
            TYPE=$(echo "$line" | awk '{print $3}')
        elif echo "$line" | grep -q "bind:"; then
            BIND=$(echo "$line" | awk '{print $2}' | tr -d '"')
        elif echo "$line" | grep -q "target:"; then
            TARGET=$(echo "$line" | awk '{print $2}' | tr -d '"')
            echo -e "    ${GRAY}${TYPE}${NC}  ${BIND} → ${TARGET}"
        fi
    done

    echo ""
    echo -e "  ${WHITE}Add new mappings:${NC}"

    # Collect new mappings
    port_mapping_wizard

    if [ "$MAP_COUNT" -gt 0 ]; then
        # Append new mappings to the maps section
        # Find the line after "maps:" and append
        local TMP_MAPS=$(printf "$MAPPINGS")

        # Use python for safe YAML append
        python3 -c "
import sys
with open('$CONFIG_FILE', 'r') as f:
    content = f.read()

new_maps = '''$TMP_MAPS'''

# Find maps section and append
if 'maps:' in content:
    # Insert before the section after maps
    parts = content.split('maps:\n')
    if len(parts) == 2:
        # Find next section (starts with non-space)
        lines = parts[1].split('\n')
        insert_at = 0
        for i, line in enumerate(lines):
            if line and not line.startswith(' ') and not line.startswith('-'):
                insert_at = i
                break
            insert_at = i + 1
        before = '\n'.join(lines[:insert_at])
        after = '\n'.join(lines[insert_at:])
        content = parts[0] + 'maps:\n' + before + new_maps + after
else:
    content += '\nmaps:\n' + new_maps

with open('$CONFIG_FILE', 'w') as f:
    f.write(content)
" 2>/dev/null

        if [ $? -eq 0 ]; then
            echo -e "  ${GREEN}$MAP_COUNT mappings added${NC}"
            echo ""
            read -p "  Restart server to apply? [Y/n]: " restart
            if [[ ! $restart =~ ^[Nn]$ ]]; then
                systemctl restart picotun-server
                echo -e "  ${GREEN}Server restarted${NC}"
            fi
        else
            # Fallback: just append raw
            printf "$MAPPINGS" >> "$CONFIG_FILE"
            echo -e "  ${GREEN}Mappings appended (manual check recommended)${NC}"
        fi
    fi

    press_enter
}

# ═════════════════════════════════════════
#  SERVICE MANAGEMENT
# ═════════════════════════════════════════

service_management() {
    local MODE=$1
    local SERVICE_NAME="picotun-${MODE}"
    local CONFIG_FILE="$CONFIG_DIR/${MODE}.yaml"

    show_banner
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}${MODE^^} MANAGEMENT${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    show_service_status "$SERVICE_NAME" "${MODE^}"
    echo ""

    echo "  1) Start ${MODE^}"
    echo "  2) Stop ${MODE^}"
    echo "  3) Restart ${MODE^}"
    echo "  4) ${MODE^} Status"
    echo "  5) View ${MODE^} Logs (Live)"
    echo "  6) Enable Auto-start"
    echo "  7) Disable Auto-start"
    echo ""
    echo "  8) View ${MODE^} Config"
    echo "  9) Edit ${MODE^} Config"
    echo "  10) Delete ${MODE^} Config & Service"

    if [ "$MODE" == "server" ]; then
        echo ""
        echo "  11) Add Port Mappings"
    fi

    if [ "$MODE" == "client" ]; then
        echo ""
        echo "  11) Add Backup IP (Anti-Block)"
    fi

    echo ""
    echo "  0) Back to Settings"
    echo ""
    read -p "  Select: " choice

    case $choice in
        1)
            systemctl start "$SERVICE_NAME"
            echo -e "  ${GREEN}${MODE^} started${NC}"
            sleep 2
            service_management "$MODE"
            ;;
        2)
            systemctl stop "$SERVICE_NAME"
            echo -e "  ${GREEN}${MODE^} stopped${NC}"
            sleep 2
            service_management "$MODE"
            ;;
        3)
            systemctl restart "$SERVICE_NAME"
            echo -e "  ${GREEN}${MODE^} restarted${NC}"
            sleep 2
            service_management "$MODE"
            ;;
        4)
            echo ""
            systemctl status "$SERVICE_NAME" --no-pager
            press_enter
            service_management "$MODE"
            ;;
        5)
            journalctl -u "$SERVICE_NAME" -f
            ;;
        6)
            systemctl enable "$SERVICE_NAME" 2>/dev/null
            echo -e "  ${GREEN}Auto-start enabled${NC}"
            sleep 2
            service_management "$MODE"
            ;;
        7)
            systemctl disable "$SERVICE_NAME" 2>/dev/null
            echo -e "  ${GREEN}Auto-start disabled${NC}"
            sleep 2
            service_management "$MODE"
            ;;
        8)
            echo ""
            if [ -f "$CONFIG_FILE" ]; then
                cat "$CONFIG_FILE"
            else
                echo -e "  ${RED}${MODE^} config not found${NC}"
            fi
            press_enter
            service_management "$MODE"
            ;;
        9)
            if [ -f "$CONFIG_FILE" ]; then
                ${EDITOR:-nano} "$CONFIG_FILE"
                echo ""
                read -p "  Restart service to apply? [y/N]: " restart
                if [[ $restart =~ ^[Yy]$ ]]; then
                    systemctl restart "$SERVICE_NAME"
                    echo -e "  ${GREEN}Service restarted${NC}"
                    sleep 2
                fi
            else
                echo -e "  ${RED}${MODE^} config not found${NC}"
                sleep 2
            fi
            service_management "$MODE"
            ;;
        10)
            read -p "  Delete ${MODE^} config and service? [y/N]: " confirm
            if [[ $confirm =~ ^[Yy]$ ]]; then
                systemctl stop "$SERVICE_NAME" 2>/dev/null
                systemctl disable "$SERVICE_NAME" 2>/dev/null
                rm -f "$CONFIG_FILE"
                rm -f "$SYSTEMD_DIR/${SERVICE_NAME}.service"
                systemctl daemon-reload
                echo -e "  ${GREEN}${MODE^} removed${NC}"
                sleep 2
            fi
            settings_menu
            ;;
        11)
            if [ "$MODE" == "server" ]; then
                add_ports_to_server
            elif [ "$MODE" == "client" ]; then
                add_backup_path
            fi
            service_management "$MODE"
            ;;
        0) settings_menu ;;
        *) service_management "$MODE" ;;
    esac
}

# ═════════════════════════════════════════
#  ADD BACKUP PATH (Client Anti-Block)
# ═════════════════════════════════════════

add_backup_path() {
    local CONFIG_FILE="$CONFIG_DIR/client.yaml"
    if [ ! -f "$CONFIG_FILE" ]; then
        echo -e "  ${RED}Client config not found${NC}"
        sleep 2
        return
    fi

    echo ""
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}ADD BACKUP IP (Anti-Block)${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${GRAY}If the main IP is blocked, PicoTun will${NC}"
    echo -e "  ${GRAY}automatically switch to the backup IP.${NC}"
    echo ""

    read -p "  Backup Server IP:Port (e.g., 5.6.7.8:2020): " BACKUP_ADDR
    if [ -z "$BACKUP_ADDR" ]; then
        echo -e "  ${RED}Address required${NC}"
        sleep 2
        return
    fi

    # Read current settings
    local TRANSPORT=$(grep "^transport:" "$CONFIG_FILE" | awk -F'"' '{print $2}')
    TRANSPORT=${TRANSPORT:-httpmux}
    local POOL_SIZE=$(grep "connection_pool:" "$CONFIG_FILE" | head -1 | awk '{print $2}')
    POOL_SIZE=${POOL_SIZE:-4}

    cat >> "$CONFIG_FILE" << EOF
  - transport: "${TRANSPORT}"
    addr: "${BACKUP_ADDR}"
    connection_pool: ${POOL_SIZE}
    retry_interval: 3
    dial_timeout: 15
EOF

    echo ""
    echo -e "  ${GREEN}Backup path added: ${BACKUP_ADDR}${NC}"

    echo ""
    read -p "  Restart client to apply? [y/N]: " restart
    if [[ $restart =~ ^[Yy]$ ]]; then
        systemctl restart picotun-client
        echo -e "  ${GREEN}Client restarted${NC}"
    fi

    echo ""
    echo -e "  ${GRAY}Current paths in config:${NC}"
    grep -A2 "addr:" "$CONFIG_FILE" | grep "addr:" | while read -r line; do
        echo -e "    ${WHITE}${line}${NC}"
    done
    echo ""
    sleep 2
}

# ═════════════════════════════════════════
#  SETTINGS MENU
# ═════════════════════════════════════════

settings_menu() {
    show_banner
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}SETTINGS${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    show_service_status "picotun-server" "Server"
    show_service_status "picotun-client" "Client"
    echo ""

    echo "  1) Manage Server"
    echo "  2) Manage Client"
    echo ""
    echo "  0) Back to Main Menu"
    echo ""
    read -p "  Select: " choice

    case $choice in
        1) service_management "server" ;;
        2) service_management "client" ;;
        0) main_menu ;;
        *) settings_menu ;;
    esac
}

# ═════════════════════════════════════════
#  UPDATE
# ═════════════════════════════════════════

update_binary() {
    show_banner
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}UPDATE PICOTUN${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""

    local CURRENT=$(get_current_version)
    echo -e "  Current version: ${WHITE}${CURRENT}${NC}"
    echo ""

    download_binary

    # Restart running services
    for svc in picotun-server picotun-client; do
        if systemctl is-active "$svc" &>/dev/null; then
            systemctl restart "$svc"
            echo -e "  ${GREEN}${svc} restarted${NC}"
        fi
    done

    press_enter
    main_menu
}

# ═════════════════════════════════════════
#  UNINSTALL
# ═════════════════════════════════════════

uninstall() {
    show_banner
    echo -e "${RED}══════════════════════════════════════════${NC}"
    echo -e "     ${RED}UNINSTALL PICOTUN${NC}"
    echo -e "${RED}══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${YELLOW}This will remove:${NC}"
    echo "    - PicoTun binary"
    echo "    - All configurations ($CONFIG_DIR)"
    echo "    - Systemd services"
    echo "    - SSL certificates"
    echo "    - System optimizations"
    echo ""
    read -p "  Are you sure? [y/N]: " confirm

    if [[ ! $confirm =~ ^[Yy]$ ]]; then
        main_menu
        return
    fi

    echo ""
    echo -e "  Stopping services..."
    systemctl stop picotun-server 2>/dev/null
    systemctl stop picotun-client 2>/dev/null
    systemctl disable picotun-server 2>/dev/null
    systemctl disable picotun-client 2>/dev/null

    rm -f "$SYSTEMD_DIR/picotun-server.service"
    rm -f "$SYSTEMD_DIR/picotun-client.service"

    echo -e "  Removing binary and configs..."
    rm -f "$INSTALL_DIR/picotun"
    rm -rf "$CONFIG_DIR"

    echo -e "  Removing optimizations..."
    rm -f /etc/sysctl.d/99-picotun.conf
    rm -f /etc/sysctl.d/99-rstunnel.conf
    sysctl -p > /dev/null 2>&1

    systemctl daemon-reload

    echo ""
    echo -e "  ${GREEN}PicoTun uninstalled successfully${NC}"
    echo ""
    exit 0
}

# ═════════════════════════════════════════
#  MAIN MENU
# ═════════════════════════════════════════

main_menu() {
    show_banner

    # Status
    show_service_status "picotun-server" "Server"
    show_service_status "picotun-client" "Client"

    local CURRENT=$(get_current_version)
    if [ "$CURRENT" != "not-installed" ]; then
        echo -e "  ${GRAY}Version: ${CURRENT}${NC}"
    fi
    echo ""

    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo -e "     ${WHITE}MAIN MENU${NC}"
    echo -e "${CYAN}══════════════════════════════════════════${NC}"
    echo ""
    echo "  1) Install Server  (Iran)"
    echo "  2) Install Client  (Kharej)"
    echo "  3) Settings (Manage Services & Configs)"
    echo "  4) System Optimizer"
    echo "  5) Update PicoTun"
    echo "  6) Uninstall PicoTun"
    echo ""
    echo "  0) Exit"
    echo ""
    read -p "  Select: " choice

    case $choice in
        1) install_server ;;
        2) install_client ;;
        3) settings_menu ;;
        4) system_optimizer_menu ;;
        5) update_binary ;;
        6) uninstall ;;
        0) echo -e "\n  ${GRAY}Goodbye!${NC}\n"; exit 0 ;;
        *) echo -e "  ${RED}Invalid option${NC}"; sleep 1; main_menu ;;
    esac
}

# ═════════════════════════════════════════
#  ENTRY POINT
# ═════════════════════════════════════════

check_root
show_banner
install_dependencies

# ── Migration: clean up old RsTunnel/rstunnel names ──
if [ -f /etc/sysctl.d/99-rstunnel.conf ]; then
    mv /etc/sysctl.d/99-rstunnel.conf /etc/sysctl.d/99-picotun.conf 2>/dev/null
    sysctl -p /etc/sysctl.d/99-picotun.conf >/dev/null 2>&1
fi

if [ ! -f "$INSTALL_DIR/picotun" ]; then
    echo -e "${YELLOW}PicoTun not found. Downloading...${NC}"
    download_binary
    echo ""
fi

main_menu
