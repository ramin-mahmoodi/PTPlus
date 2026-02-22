package httpmux

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ═══════════════════════════════════════════════════════════════
//  PicoTun v2.5 — Configuration
//
//  Changes from v2.4:
//    • Multi-port listen (load balancer across Iran IPs/ports)
//    • Config version + auto-migration
//    • DPI stealth settings
//    • Higher capacity defaults (120+ users)
//    • Improved connection stability
// ═══════════════════════════════════════════════════════════════

const CurrentConfigVersion = 2

type Config struct {
	ConfigVersion int    `yaml:"config_version"`
	Mode          string `yaml:"mode"`
	Listen        string `yaml:"listen"`
	Transport     string `yaml:"transport"`
	PSK           string `yaml:"psk"`
	Profile       string `yaml:"profile"`
	Verbose       bool   `yaml:"verbose"`
	CertFile      string `yaml:"cert_file"`
	KeyFile       string `yaml:"key_file"`
	MaxSessions   int    `yaml:"max_sessions"`
	Heartbeat     int    `yaml:"heartbeat"`

	NumConnections   int  `yaml:"num_connections"`
	EnableDecoy      bool `yaml:"enable_decoy"`
	DecoyInterval    int  `yaml:"decoy_interval"`
	EmbedFakeHeaders bool `yaml:"embed_fake_headers"`

	// ─── Multi-Port Load Balancer (v2.5) ───
	ListenPorts []string `yaml:"listen_ports"`

	Maps  []PortMap    `yaml:"maps"`
	Paths []PathConfig `yaml:"paths"`

	Smux        SmuxConfig      `yaml:"smux"`
	KCP         KCPConfig       `yaml:"kcp"`
	Advanced    AdvancedConfig  `yaml:"advanced"`
	Obfuscation ObfsCompat      `yaml:"obfuscation"`
	HTTPMimic   HTTPMimicCompat `yaml:"http_mimic"`
	Fragment    FragmentConfig  `yaml:"fragment"`

	// ─── DPI Stealth (v2.5) ───
	Stealth StealthConfig `yaml:"stealth"`

	ServerURL string `yaml:"server_url"`
	SessionID string `yaml:"session_id"`

	Forward struct {
		TCP []string `yaml:"tcp"`
		UDP []string `yaml:"udp"`
	} `yaml:"forward"`

	Mimic MimicConfig `yaml:"mimic"`
	Obfs  ObfsConfig  `yaml:"obfs"`

	SessionTimeout int `yaml:"session_timeout"`
}

type StealthConfig struct {
	RandomPadding       bool `yaml:"random_padding"`
	MinPadding          int  `yaml:"min_padding"`
	MaxPadding          int  `yaml:"max_padding"`
	KeepaliveJitter     int  `yaml:"keepalive_jitter"`
	ConnJitterMS        int  `yaml:"conn_jitter_ms"`
	BurstSplit          bool `yaml:"burst_split"`
	MaxBurstSize        int  `yaml:"max_burst_size"`
	FakeTraffic         bool `yaml:"fake_traffic"`
	FakeTrafficInterval int  `yaml:"fake_traffic_interval"`
}

type PathConfig struct {
	Transport      string `yaml:"transport"`
	Addr           string `yaml:"addr"`
	ConnectionPool int    `yaml:"connection_pool"`
	AggressivePool bool   `yaml:"aggressive_pool"`
	RetryInterval  int    `yaml:"retry_interval"`
	DialTimeout    int    `yaml:"dial_timeout"`
}

type PortMap struct {
	Type   string `yaml:"type"`
	Bind   string `yaml:"bind"`
	Target string `yaml:"target"`
}

type SmuxConfig struct {
	KeepAlive int `yaml:"keepalive"`
	MaxRecv   int `yaml:"max_recv"`
	MaxStream int `yaml:"max_stream"`
	FrameSize int `yaml:"frame_size"`
	Version   int `yaml:"version"`
}

type KCPConfig struct {
	NoDelay  int `yaml:"nodelay"`
	Interval int `yaml:"interval"`
	Resend   int `yaml:"resend"`
	NC       int `yaml:"nc"`
	SndWnd   int `yaml:"sndwnd"`
	RcvWnd   int `yaml:"rcvwnd"`
	MTU      int `yaml:"mtu"`
}

type AdvancedConfig struct {
	TCPNoDelay           bool `yaml:"tcp_nodelay"`
	TCPKeepAlive         int  `yaml:"tcp_keepalive"`
	TCPReadBuffer        int  `yaml:"tcp_read_buffer"`
	TCPWriteBuffer       int  `yaml:"tcp_write_buffer"`
	WebSocketReadBuffer  int  `yaml:"websocket_read_buffer"`
	WebSocketWriteBuffer int  `yaml:"websocket_write_buffer"`
	WebSocketCompression bool `yaml:"websocket_compression"`
	CleanupInterval      int  `yaml:"cleanup_interval"`
	SessionTimeout       int  `yaml:"session_timeout"`
	ConnectionTimeout    int  `yaml:"connection_timeout"`
	StreamTimeout        int  `yaml:"stream_timeout"`
	MaxConnections       int  `yaml:"max_connections"`
	MaxUDPFlows          int  `yaml:"max_udp_flows"`
	UDPFlowTimeout       int  `yaml:"udp_flow_timeout"`
	UDPBufferSize        int  `yaml:"udp_buffer_size"`
	MaxStreamsPerSession int  `yaml:"max_streams_per_session"`
}

type HTTPMimicCompat struct {
	FakeDomain      string   `yaml:"fake_domain"`
	FakePath        string   `yaml:"fake_path"`
	UserAgent       string   `yaml:"user_agent"`
	ChunkedEncoding bool     `yaml:"chunked_encoding"`
	SessionCookie   bool     `yaml:"session_cookie"`
	CustomHeaders   []string `yaml:"custom_headers"`
}

type ObfsCompat struct {
	Enabled     bool    `yaml:"enabled"`
	MinPadding  int     `yaml:"min_padding"`
	MaxPadding  int     `yaml:"max_padding"`
	MinDelayMS  int     `yaml:"min_delay_ms"`
	MaxDelayMS  int     `yaml:"max_delay_ms"`
	BurstChance float64 `yaml:"burst_chance"`
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func applyBaseDefaults(c *Config) {
	if c.Profile == "" {
		c.Profile = "balanced"
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 2
	}
	if c.SessionTimeout <= 0 {
		c.SessionTimeout = 30
	}
	if c.Advanced.SessionTimeout > 0 {
		c.SessionTimeout = c.Advanced.SessionTimeout
	}

	if c.Smux.KeepAlive <= 0 {
		c.Smux.KeepAlive = 2
	}
	if c.Smux.MaxRecv <= 0 {
		// 4 MB — needed to sustain gigabit throughput; the old 1 MB default
		// caused smux flow-control to stall at ~50 Mbps per stream.
		c.Smux.MaxRecv = 4194304
	}
	if c.Smux.MaxStream <= 0 {
		c.Smux.MaxStream = 4194304
	}
	if c.Smux.FrameSize <= 0 {
		c.Smux.FrameSize = 4096
	}
	if c.Smux.Version <= 0 {
		c.Smux.Version = 2
	}

	if c.Advanced.TCPKeepAlive <= 0 {
		c.Advanced.TCPKeepAlive = 5
	}
	if c.Advanced.TCPReadBuffer <= 0 {
		// 4 MB — a 64 KB socket buffer limits throughput to roughly
		// 64 KB / RTT. At a 10 ms RTT that caps at ~51 Mbps. 4 MB raises
		// the ceiling well above 1 Gbps.
		c.Advanced.TCPReadBuffer = 4194304
	}
	if c.Advanced.TCPWriteBuffer <= 0 {
		c.Advanced.TCPWriteBuffer = 4194304
	}
	if c.Advanced.CleanupInterval <= 0 {
		c.Advanced.CleanupInterval = 3
	}
	if c.Advanced.ConnectionTimeout <= 0 {
		c.Advanced.ConnectionTimeout = 30
	}
	if c.Advanced.StreamTimeout <= 0 {
		c.Advanced.StreamTimeout = 60
	}
	if c.Advanced.MaxConnections <= 0 {
		c.Advanced.MaxConnections = 500
	}
	if c.Advanced.MaxUDPFlows <= 0 {
		c.Advanced.MaxUDPFlows = 300
	}
	if c.Advanced.UDPFlowTimeout <= 0 {
		c.Advanced.UDPFlowTimeout = 120
	}
	if c.Advanced.UDPBufferSize <= 0 {
		c.Advanced.UDPBufferSize = 524288
	}
	if c.Advanced.MaxStreamsPerSession <= 0 {
		c.Advanced.MaxStreamsPerSession = 512
	}
	c.Advanced.TCPNoDelay = true

	if c.HTTPMimic.FakeDomain == "" {
		c.HTTPMimic.FakeDomain = "www.google.com"
	}
	if c.HTTPMimic.FakePath == "" {
		c.HTTPMimic.FakePath = "/search"
	}
	c.HTTPMimic.FakePath = normalizePath(c.HTTPMimic.FakePath)
	if c.HTTPMimic.UserAgent == "" {
		c.HTTPMimic.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	if !c.HTTPMimic.SessionCookie {
		c.HTTPMimic.SessionCookie = true
	}

	if c.Obfuscation.MinPadding <= 0 {
		c.Obfuscation.MinPadding = 16
	}
	if c.Obfuscation.MaxPadding <= 0 {
		c.Obfuscation.MaxPadding = 64
	}

	if c.NumConnections <= 0 {
		c.NumConnections = 4
	}
	if c.DecoyInterval <= 0 {
		c.DecoyInterval = 5
	}

	if c.Fragment.MinSize <= 0 {
		c.Fragment.MinSize = 64
	}
	if c.Fragment.MaxSize <= 0 {
		c.Fragment.MaxSize = 191
	}
	if c.Fragment.MinDelay <= 0 {
		c.Fragment.MinDelay = 1
	}
	if c.Fragment.MaxDelay <= 0 {
		c.Fragment.MaxDelay = 3
	}
	transport := strings.ToLower(c.Transport)
	if !c.Fragment.Enabled && (transport == "httpsmux" || transport == "wssmux") {
		c.Fragment.Enabled = true
	}

	// DPI Stealth defaults
	if c.Stealth.MinPadding <= 0 {
		c.Stealth.MinPadding = 16
	}
	if c.Stealth.MaxPadding <= 0 {
		c.Stealth.MaxPadding = 128
	}
	if c.Stealth.KeepaliveJitter <= 0 {
		c.Stealth.KeepaliveJitter = 2
	}
	if c.Stealth.ConnJitterMS <= 0 {
		c.Stealth.ConnJitterMS = 500
	}
	if c.Stealth.MaxBurstSize <= 0 {
		c.Stealth.MaxBurstSize = 4096
	}
	if c.Stealth.FakeTrafficInterval <= 0 {
		c.Stealth.FakeTrafficInterval = 30
	}

	// Multi-port: merge Listen into ListenPorts
	if c.Mode == "server" {
		if len(c.ListenPorts) == 0 && c.Listen != "" {
			c.ListenPorts = []string{c.Listen}
		}
		if len(c.ListenPorts) == 0 {
			c.ListenPorts = []string{"0.0.0.0:2020"}
			c.Listen = "0.0.0.0:2020"
		}
		if c.Listen == "" {
			c.Listen = c.ListenPorts[0]
		}
	}
}

func applyProfile(c *Config) {
	switch strings.ToLower(c.Profile) {
	case "speed", "aggressive":
		c.Obfuscation.MinDelayMS = 0
		c.Obfuscation.MaxDelayMS = 0
		c.HTTPMimic.ChunkedEncoding = false
		for i := range c.Paths {
			if c.Paths[i].ConnectionPool < 4 {
				c.Paths[i].ConnectionPool = 4
			}
			c.Paths[i].AggressivePool = true
			if c.Paths[i].RetryInterval <= 0 || c.Paths[i].RetryInterval > 2 {
				c.Paths[i].RetryInterval = 2
			}
			if c.Paths[i].DialTimeout <= 0 {
				c.Paths[i].DialTimeout = 10
			}
		}

	case "gaming", "latency":
		c.Smux.KeepAlive = 1
		c.Advanced.TCPKeepAlive = 2
		c.Obfuscation.MinDelayMS = 0
		c.Obfuscation.MaxDelayMS = 0
		c.HTTPMimic.ChunkedEncoding = false
		c.Stealth.RandomPadding = false
		c.Stealth.BurstSplit = false
		for i := range c.Paths {
			if c.Paths[i].ConnectionPool < 6 {
				c.Paths[i].ConnectionPool = 6
			}
			c.Paths[i].AggressivePool = true
			if c.Paths[i].RetryInterval <= 0 || c.Paths[i].RetryInterval > 1 {
				c.Paths[i].RetryInterval = 1
			}
			if c.Paths[i].DialTimeout <= 0 {
				c.Paths[i].DialTimeout = 5
			}
		}

	case "streaming":
		c.Smux.MaxRecv = 2097152
		c.Smux.MaxStream = 2097152
		c.Obfuscation.MinDelayMS = 0
		c.Obfuscation.MaxDelayMS = 0
		for i := range c.Paths {
			if c.Paths[i].ConnectionPool < 4 {
				c.Paths[i].ConnectionPool = 4
			}
			if c.Paths[i].RetryInterval <= 0 {
				c.Paths[i].RetryInterval = 2
			}
			if c.Paths[i].DialTimeout <= 0 {
				c.Paths[i].DialTimeout = 10
			}
		}

	case "lowcpu", "cpu-efficient":
		c.Smux.KeepAlive = 5
		c.Smux.MaxRecv = 524288
		c.Smux.MaxStream = 524288
		c.Stealth.FakeTraffic = false
		for i := range c.Paths {
			if c.Paths[i].ConnectionPool <= 0 || c.Paths[i].ConnectionPool > 2 {
				c.Paths[i].ConnectionPool = 2
			}
			if c.Paths[i].RetryInterval <= 0 {
				c.Paths[i].RetryInterval = 5
			}
			if c.Paths[i].DialTimeout <= 0 {
				c.Paths[i].DialTimeout = 15
			}
		}

	default:
		c.Obfuscation.MinDelayMS = 0
		c.Obfuscation.MaxDelayMS = 0
		for i := range c.Paths {
			if c.Paths[i].ConnectionPool <= 0 {
				c.Paths[i].ConnectionPool = 4
			}
			if c.Paths[i].RetryInterval <= 0 {
				c.Paths[i].RetryInterval = 3
			}
			if c.Paths[i].DialTimeout <= 0 {
				c.Paths[i].DialTimeout = 10
			}
		}
	}
}

func syncAliases(c *Config) {
	if c.Mimic.FakeDomain == "" {
		c.Mimic.FakeDomain = c.HTTPMimic.FakeDomain
	}
	if c.Mimic.FakePath == "" {
		c.Mimic.FakePath = c.HTTPMimic.FakePath
	}
	if c.Mimic.UserAgent == "" {
		c.Mimic.UserAgent = c.HTTPMimic.UserAgent
	}
	if len(c.Mimic.CustomHeaders) == 0 && len(c.HTTPMimic.CustomHeaders) > 0 {
		c.Mimic.CustomHeaders = append([]string{}, c.HTTPMimic.CustomHeaders...)
	}
	c.Mimic.Chunked = c.HTTPMimic.ChunkedEncoding
	c.Mimic.SessionCookie = c.HTTPMimic.SessionCookie

	if !c.Obfs.Enabled {
		c.Obfs.Enabled = c.Obfuscation.Enabled
	}
	if c.Obfs.MinPadding <= 0 {
		c.Obfs.MinPadding = c.Obfuscation.MinPadding
	}
	if c.Obfs.MaxPadding <= 0 {
		c.Obfs.MaxPadding = c.Obfuscation.MaxPadding
	}
	if c.Obfs.MinDelayMS <= 0 {
		c.Obfs.MinDelayMS = c.Obfuscation.MinDelayMS
	}
	if c.Obfs.MaxDelayMS <= 0 {
		c.Obfs.MaxDelayMS = c.Obfuscation.MaxDelayMS
	}
	if c.Obfs.BurstChance <= 0 {
		c.Obfs.BurstChance = int(c.Obfuscation.BurstChance * 1000)
	}
}

func convertMapsToForward(c *Config) {
	if len(c.Forward.TCP) == 0 && len(c.Forward.UDP) == 0 {
		for _, m := range c.Maps {
			entry := strings.TrimSpace(m.Bind) + "->" + strings.TrimSpace(m.Target)
			switch strings.ToLower(strings.TrimSpace(m.Type)) {
			case "udp":
				c.Forward.UDP = append(c.Forward.UDP, entry)
			case "both":
				c.Forward.TCP = append(c.Forward.TCP, entry)
				c.Forward.UDP = append(c.Forward.UDP, entry)
			default:
				c.Forward.TCP = append(c.Forward.TCP, entry)
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════
// Config Migration — auto-update old configs to new format
// ═══════════════════════════════════════════════════════════════

func migrateConfig(c *Config, path string) {
	if c.ConfigVersion >= CurrentConfigVersion {
		return
	}
	log.Printf("[CONFIG] migrating config v%d → v%d", c.ConfigVersion, CurrentConfigVersion)

	if c.ConfigVersion < 2 {
		if c.Smux.MaxRecv == 524288 || c.Smux.MaxRecv == 1048576 {
			c.Smux.MaxRecv = 4194304
			log.Printf("[CONFIG]   smux.max_recv: old → 4MB")
		}
		if c.Smux.MaxStream == 524288 || c.Smux.MaxStream == 1048576 {
			c.Smux.MaxStream = 4194304
			log.Printf("[CONFIG]   smux.max_stream: old → 4MB")
		}
		if c.Smux.FrameSize == 2048 {
			c.Smux.FrameSize = 4096
			log.Printf("[CONFIG]   smux.frame_size: 2KB → 4KB")
		}
		if c.Smux.KeepAlive == 1 && c.Profile != "gaming" {
			c.Smux.KeepAlive = 2
			log.Printf("[CONFIG]   smux.keepalive: 1s → 2s")
		}
		if c.Advanced.TCPReadBuffer == 32768 || c.Advanced.TCPReadBuffer == 65536 {
			c.Advanced.TCPReadBuffer = 4194304
			log.Printf("[CONFIG]   tcp_read_buffer: old → 4MB")
		}
		if c.Advanced.TCPWriteBuffer == 32768 || c.Advanced.TCPWriteBuffer == 65536 {
			c.Advanced.TCPWriteBuffer = 4194304
			log.Printf("[CONFIG]   tcp_write_buffer: old → 4MB")
		}
		if c.SessionTimeout == 15 {
			c.SessionTimeout = 30
			log.Printf("[CONFIG]   session_timeout: 15s → 30s")
		}
		if !c.Stealth.RandomPadding {
			c.Stealth.RandomPadding = true
			log.Printf("[CONFIG]   stealth.random_padding: enabled")
		}
	}

	c.ConfigVersion = CurrentConfigVersion
	if path != "" {
		if err := SaveConfig(c, path); err != nil {
			log.Printf("[CONFIG] warning: could not save migrated config: %v", err)
		} else {
			log.Printf("[CONFIG] migrated config saved to %s", path)
		}
	}
}

func SaveConfig(c *Config, path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}

	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	c.Profile = strings.ToLower(strings.TrimSpace(c.Profile))
	c.Listen = strings.TrimSpace(c.Listen)
	c.ServerURL = strings.TrimSpace(c.ServerURL)

	if c.Mode == "server" && c.Listen == "" && len(c.ListenPorts) == 0 {
		c.Listen = "0.0.0.0:2020"
	}

	applyBaseDefaults(&c)
	applyProfile(&c)
	convertMapsToForward(&c)
	syncAliases(&c)
	migrateConfig(&c, path)

	return &c, nil
}
