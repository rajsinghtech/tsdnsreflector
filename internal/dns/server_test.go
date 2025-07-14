package dns

import (
	"net"
	"net/netip"
	"testing"

	"github.com/miekg/dns"
	via6 "github.com/rajsingh/tsdnsreflector/internal/4via6"
	"github.com/rajsingh/tsdnsreflector/internal/cache"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    3,
			},
		},
		Zones: map[string]*config.Zone{
			"test": {
				Domains:         []string{"*.test.local"},
				ReflectedDomain: "backend.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
			},
		},
	}

	// Use the deprecated NewServer which creates default runtime config
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server.config == nil {
		t.Error("Server config should be set")
	}
	if server.via6Trans == nil {
		t.Error("4via6 translator should be initialized")
	}
	if server.forwarder == nil {
		t.Error("Forwarder should be initialized")
	}
	if server.handler == nil {
		t.Error("Handler should be initialized")
	}
	if server.dnsServer == nil {
		t.Error("DNS server should be initialized")
	}
}

func TestNewServerWithRuntime(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    3,
			},
		},
		Zones: map[string]*config.Zone{},
	}

	runtimeCfg := &config.RuntimeConfig{
		Hostname:    "test-server",
		DNSPort:     5353,
		BindAddress: "127.0.0.1",
		DefaultTTL:  300,
		TSAuthKey:   "test-auth-key",
		TSHostname:  "test-ts-server",
		TSStateDir:  "/tmp/tailscale",
	}

	server, err := NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		t.Fatalf("Failed to create server with runtime config: %v", err)
	}

	if server.tsnetServer == nil {
		t.Error("TSNet server should be initialized when auth key provided")
	}
}

func TestNewServerWithInvalidVia6Config(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    3,
			},
		},
		Zones: map[string]*config.Zone{
			"test": {
				Domains:         []string{"*.test.local"},
				ReflectedDomain: "backend.local",
				PrefixSubnet:    "invalid-subnet",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
			},
		},
	}

	// Use the deprecated NewServer
	_, err := NewServer(cfg)
	if err == nil {
		t.Error("Expected error for invalid 4via6 config")
	}
}

func TestDNSHandler_ServeDNS_Via6Query(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    3,
			},
		},
	}

	cfg.Zones = map[string]*config.Zone{
		"cluster": {
			Domains:         []string{"*.cluster.local"},
			ReflectedDomain: "127.0.0.1",
			PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
			TranslateID:     func() *uint16 { v := uint16(42); return &v }(),
			Backend: cfg.Global.Backend,
		},
	}

	// Create runtime config with test values
	runtimeCfg := &config.RuntimeConfig{
		DefaultTTL: 300,
		LogQueries: false,
	}

	// Create components manually for test
	loggingCfg := runtimeCfg.ToLoggingConfig()
	log := logger.New(loggingCfg)
	
	via6Trans, err := via6.NewTranslator(cfg, log)
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	forwarder := NewForwarder(cfg.Global.Backend, log)
	
	handler := &TailscaleDNSHandler{
		config:     cfg,
		runtimeCfg: runtimeCfg,
		via6Trans:  via6Trans,
		forwarder:  forwarder,
		logger:     log,
		zoneCaches: make(map[string]*cache.ZoneCache),
	}

	// Test AAAA query for 4via6
	req := &dns.Msg{
		Question: []dns.Question{
			{
				Name:   "test.cluster.local.",
				Qtype:  dns.TypeAAAA,
				Qclass: dns.ClassINET,
			},
		},
	}

	w := &testResponseWriter{
		remoteAddr: &net.UDPAddr{IP: net.ParseIP("100.64.0.1"), Port: 53},
	}

	handler.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("Expected response message")
	}

	if len(w.msg.Answer) != 1 {
		t.Fatalf("Expected 1 answer, got %d", len(w.msg.Answer))
	}

	aaaa, ok := w.msg.Answer[0].(*dns.AAAA)
	if !ok {
		t.Fatalf("Expected AAAA record, got %T", w.msg.Answer[0])
	}

	// The response should contain a valid IPv6 address
	if aaaa.AAAA == nil {
		t.Error("Expected valid IPv6 address in AAAA record")
	}
}

func TestDNSHandler_ServeDNS_NonTailscaleClient(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    3,
			},
		},
		Zones: map[string]*config.Zone{
			"cluster": {
				Domains:         []string{"*.cluster.local"},
				ReflectedDomain: "backend.local",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
				Backend:         config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
			},
		},
	}

	// Create runtime config
	runtimeCfg := &config.RuntimeConfig{
		DefaultTTL: 300,
		LogQueries: false,
	}

	// Create components manually for test
	loggingCfg := runtimeCfg.ToLoggingConfig()
	log := logger.New(loggingCfg)
	
	via6Trans, err := via6.NewTranslator(cfg, log)
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	forwarder := NewForwarder(cfg.Global.Backend, log)
	
	handler := &TailscaleDNSHandler{
		config:     cfg,
		runtimeCfg: runtimeCfg,
		via6Trans:  via6Trans,
		forwarder:  forwarder,
		logger:     log,
		zoneCaches: make(map[string]*cache.ZoneCache),
	}

	// Test query from non-Tailscale client (external IP)
	req := &dns.Msg{
		Question: []dns.Question{
			{
				Name:   "test.cluster.local.",
				Qtype:  dns.TypeA,
				Qclass: dns.ClassINET,
			},
		},
	}

	w := &testResponseWriter{
		remoteAddr: &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 53}, // External IP
	}

	handler.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("Expected response message")
	}

	// Non-Tailscale clients should get NXDOMAIN for non-MagicDNS queries
	if w.msg.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN for non-Tailscale client, got %v", dns.RcodeToString[w.msg.Rcode])
	}
}

// testResponseWriter implements dns.ResponseWriter for testing
type testResponseWriter struct {
	msg        *dns.Msg
	remoteAddr net.Addr
}

func (w *testResponseWriter) LocalAddr() net.Addr        { return nil }
func (w *testResponseWriter) RemoteAddr() net.Addr       { return w.remoteAddr }
func (w *testResponseWriter) WriteMsg(m *dns.Msg) error  { w.msg = m; return nil }
func (w *testResponseWriter) Write([]byte) (int, error)  { return 0, nil }
func (w *testResponseWriter) Close() error               { return nil }
func (w *testResponseWriter) TsigStatus() error          { return nil }
func (w *testResponseWriter) TsigTimersOnly(bool)        {}
func (w *testResponseWriter) Hijack()                    {}

func TestClientDetection(t *testing.T) {
	handler := &TailscaleDNSHandler{}

	tests := []struct {
		name              string
		ip                string
		expectTailscale   bool
	}{
		{"Tailscale IPv4", "100.64.0.1", true},
		{"Tailscale IPv4 upper range", "100.127.255.254", true},
		{"Non-Tailscale IPv4", "8.8.8.8", false},
		{"Loopback IPv4", "127.0.0.1", true},
		{"Tailscale IPv6", "fd7a:115c:a1e0::1", true},
		{"Non-Tailscale IPv6", "2001:4860:4860::8888", false},
		{"Loopback IPv6", "::1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := netip.ParseAddr(tt.ip)
			if err != nil {
				t.Fatalf("Failed to parse IP %s: %v", tt.ip, err)
			}

			result := handler.isTailscaleClient(ip)
			if result != tt.expectTailscale {
				t.Errorf("isTailscaleClient(%s) = %v, want %v", tt.ip, result, tt.expectTailscale)
			}
		})
	}
}

func TestForwarder_ExchangeViaTSNet(t *testing.T) {
	// This test would require a mock TSNet server
	// For now, we'll test that the forwarder can be created with TSNet
	cfg := config.BackendConfig{
		DNSServers: []string{"10.0.0.10:53"},
		Timeout:    "5s",
		Retries:    3,
	}

	logger := logger.Default()
	forwarder := NewForwarder(cfg, logger)

	if forwarder.tsnetServer != nil {
		t.Error("Expected nil tsnetServer without TSNet")
	}

	// Test with TSNet (would need mock)
	// mockTSNet := &mockTSNetServer{}
	// forwarderWithTSNet := NewForwarderWithTSNet(cfg, logger, mockTSNet)
	// if forwarderWithTSNet.tsnetServer == nil {
	//     t.Error("Expected TSNet server to be set")
	// }
}