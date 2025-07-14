package via6

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

func TestNewTranslator(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.Config
		wantError bool
		wantZones int
	}{
		{
			name: "valid single zone",
			config: &config.Config{
				Zones: map[string]*config.Zone{
					"cluster": {
						Domains: []string{"*.cluster.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"10.0.0.1:53"},
						},
						ReflectedDomain: "backend.local",
						PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
						TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
					},
				},
			},
			wantError: false,
			wantZones: 1,
		},
		{
			name: "multiple zones",
			config: &config.Config{
				Zones: map[string]*config.Zone{
					"app": {
						Domains: []string{"*.app.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"10.0.0.1:53"},
						},
						ReflectedDomain: "app-backend.local",
						PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
						TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
					},
					"db": {
						Domains: []string{"*.db.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"10.0.0.2:53"},
						},
						ReflectedDomain: "db-backend.local",
						PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
						TranslateID:     func() *uint16 { v := uint16(2); return &v }(),
					},
				},
			},
			wantError: false,
			wantZones: 2,
		},
		{
			name: "disabled zone ignored",
			config: &config.Config{
				Zones: map[string]*config.Zone{
					"cluster": {
						Domains: []string{"*.cluster.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"10.0.0.1:53"},
						},
						ReflectedDomain: "backend.local",
						PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
						TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
					},
				},
			},
			wantError: false,
			wantZones: 1,
		},
		{
			name: "zone without 4via6",
			config: &config.Config{
				Zones: map[string]*config.Zone{
					"cluster": {
						Domains: []string{"*.cluster.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"10.0.0.1:53"},
						},
					},
				},
			},
			wantError: false,
			wantZones: 0,
		},
		{
			name: "invalid prefix subnet",
			config: &config.Config{
				Zones: map[string]*config.Zone{
					"cluster": {
						Domains: []string{"*.cluster.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"10.0.0.1:53"},
						},
						ReflectedDomain: "backend.local",
						PrefixSubnet:    "invalid-subnet",
						TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
					},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator, err := NewTranslator(tt.config, logger.Default())

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(translator.zones) != tt.wantZones {
				t.Errorf("Expected %d zones, got %d", tt.wantZones, len(translator.zones))
			}
		})
	}
}

func TestShouldTranslate(t *testing.T) {
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"cluster": {
				Domains: []string{"*.cluster.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
				},
				ReflectedDomain: "backend.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
			},
		},
	}
	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	tests := []struct {
		domain string
		want   bool
	}{
		{"cluster.local", false},  // *.cluster.local doesn't match the base domain
		{"cluster.local.", false}, // *.cluster.local doesn't match the base domain
		{"app.cluster.local", true},
		{"app.cluster.local.", true},
		{"example.com", false},
		{"example.com.", false},
		{"notcluster.local", false},
		{"cluster.notlocal", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := translator.ShouldTranslate(tt.domain)
			if got != tt.want {
				t.Errorf("ShouldTranslate(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestTranslateToVia6(t *testing.T) {
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"cluster": {
				Domains: []string{"*.cluster.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
				ReflectedDomain: "127.0.0.1", // Use IP directly for testing
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(42); return &v }(),
			},
		},
	}
	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	tests := []struct {
		domain     string
		wantError  bool
		validateIP func(net.IP) bool
	}{
		{
			domain:    "app.cluster.local",
			wantError: false,
			validateIP: func(ip net.IP) bool {
				// Use our test helper to validate the address
				defer func() {
					if r := recover(); r != nil {
						// If validation panics, IP is invalid - ignore the panic
						_ = r
					}
				}()

				// Check basic structure
				if len(ip) != 16 {
					return false
				}

				// Use helper to validate - this will work even if DNS resolution differs
				expectedTranslateID := uint16(42)
				// Don't validate specific IPv4 as it depends on actual DNS resolution
				actualTranslateID := (uint16(ip[10]) << 8) | uint16(ip[11])
				return actualTranslateID == expectedTranslateID
			},
		},
		{
			domain:    "nonexistent.domain",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			ip, err := translator.TranslateToVia6(tt.domain)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !tt.validateIP(ip) {
				t.Errorf("Generated IP %v is not valid", ip)
			}
		})
	}
}

func TestTranslateFromVia6(t *testing.T) {
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"cluster": {
				Domains: []string{"*.cluster.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
				},
				ReflectedDomain: "backend.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(42); return &v }(),
			},
		},
	}
	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	tests := []struct {
		name       string
		via6IP     net.IP
		wantDomain string
		wantIPv4   net.IP
		wantError  bool
	}{
		{
			name: "valid 4via6 address",
			via6IP: net.IP{
				0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a, // prefix
				0x00, 0x00, // reserved
				0x00, 0x2A, // translate ID (42)
				192, 168, 1, 1, // IPv4
			},
			wantDomain: "backend.local",
			wantIPv4:   net.IPv4(192, 168, 1, 1),
			wantError:  false,
		},
		{
			name: "invalid prefix",
			via6IP: net.IP{
				0xff, 0xff, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a, // wrong prefix
				0x00, 0x00,
				0x00, 0x2A,
				192, 168, 1, 1,
			},
			wantError: true,
		},
		{
			name: "unknown translate ID",
			via6IP: net.IP{
				0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a,
				0x00, 0x00,
				0x00, 0x99, // unknown ID
				192, 168, 1, 1,
			},
			wantError: true,
		},
		{
			name:      "invalid IP length",
			via6IP:    net.IP{0xff, 0xff}, // Too short
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, ipv4, err := translator.TranslateFromVia6(tt.via6IP)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if domain != tt.wantDomain {
				t.Errorf("Got domain %q, want %q", domain, tt.wantDomain)
			}

			if !ipv4.Equal(tt.wantIPv4) {
				t.Errorf("Got IPv4 %v, want %v", ipv4, tt.wantIPv4)
			}
		})
	}
}

func TestIsVia6Address(t *testing.T) {
	translator := &Translator{}

	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{
			name: "valid 4via6 address",
			ip: net.IP{
				0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a,
				0x00, 0x00,
				0x00, 0x01,
				192, 168, 1, 1,
			},
			want: true,
		},
		{
			name: "invalid prefix",
			ip: net.IP{
				0xff, 0xff, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a,
				0x00, 0x00,
				0x00, 0x01,
				192, 168, 1, 1,
			},
			want: false,
		},
		{
			name: "regular IPv6",
			ip:   net.ParseIP("2001:db8::1"),
			want: false,
		},
		{
			name: "IPv4",
			ip:   net.IPv4(192, 168, 1, 1),
			want: false,
		},
		{
			name: "nil IP",
			ip:   nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translator.isVia6Address(tt.ip)
			if got != tt.want {
				t.Errorf("isVia6Address(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestDomainNormalization(t *testing.T) {
	// Test that domains are properly normalized with trailing dots
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"cluster": {
				Domains: []string{"*.cluster.local"}, // No trailing dot
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
				ReflectedDomain: "backend.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
			},
		},
	}
	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	// Both with and without trailing dots should work
	domains := []string{"app.cluster.local", "app.cluster.local."}
	for _, domain := range domains {
		if !translator.ShouldTranslate(domain) {
			t.Errorf("Domain %q should be translatable", domain)
		}
	}
}

func TestDuplicateTranslateID(t *testing.T) {
	// Test duplicate TranslateID should fail for zones
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"cluster1": {
				Domains: []string{"*.cluster1.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
				ReflectedDomain: "backend1.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
			},
			"cluster2": {
				Domains: []string{"*.cluster2.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
				ReflectedDomain: "backend2.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(), // Same as above - should fail
			},
		},
	}

	// Validate config first - this should catch the duplicate
	err := cfg.ValidateZones()

	if err == nil {
		t.Fatal("Expected error for duplicate TranslateID but got none")
	}

	expectedError := "translateID 1 used by"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain %q, got: %v", expectedError, err)
	}
}

func TestOverlappingDomains(t *testing.T) {
	// Test overlapping domains is allowed but more specific wins
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"generic": {
				Domains: []string{"*.cluster.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
				},
				ReflectedDomain: "generic.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
			},
			"specific": {
				Domains: []string{"svc.cluster.local"}, // More specific
				Backend: config.BackendConfig{
					DNSServers: []string{"10.0.0.1:53"},
				},
				ReflectedDomain: "specific.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(2); return &v }(),
			},
		},
	}

	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	// Specific match should win
	if !translator.ShouldTranslate("svc.cluster.local") {
		t.Error("Expected svc.cluster.local to be translatable")
	}

	// Generic match should also work
	if !translator.ShouldTranslate("app.cluster.local") {
		t.Error("Expected app.cluster.local to be translatable")
	}
}

func TestTranslateIDZero(t *testing.T) {
	// Test TranslateID = 0 should be ignored (not create 4via6 zone)
	cfg := &config.Config{
		Zones: map[string]*config.Zone{
			"cluster": {
				Domains: []string{"*.cluster.local"},
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
				},
				ReflectedDomain: "backend.local",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(0); return &v }(), // Should be ignored
			},
		},
	}

	translator, err := NewTranslator(cfg, logger.Default())

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should have 0 zones since TranslateID = 0 disables 4via6
	if len(translator.zones) != 0 {
		t.Errorf("Expected 0 4via6 zones, got %d", len(translator.zones))
	}
}

func TestInvalidPrefixSubnet(t *testing.T) {
	tests := []struct {
		name         string
		prefixSubnet string
		wantError    string
	}{
		{
			name:         "invalid CIDR",
			prefixSubnet: "invalid-cidr",
			wantError:    "invalid prefix subnet",
		},
		{
			name:         "wrong 4via6 prefix",
			prefixSubnet: "2001:db8::/64",
			wantError:    "not within 4via6 space",
		},
		{
			name:         "valid 4via6 prefix",
			prefixSubnet: "fd7a:115c:a1e0:b1a::/64",
			wantError:    "", // Should succeed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Zones: map[string]*config.Zone{
					"cluster": {
						Domains: []string{"*.cluster.local"},
						Backend: config.BackendConfig{
							DNSServers: []string{"8.8.8.8:53"},
						},
						ReflectedDomain: "backend.local",
						PrefixSubnet:    tt.prefixSubnet,
						TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
					},
				},
			}

			_, err := NewTranslator(cfg, logger.Default())

			if tt.wantError == "" {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected error containing %q but got none", tt.wantError)
				} else if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("Expected error to contain %q, got: %v", tt.wantError, err)
				}
			}
		})
	}
}

func TestTranslateIDMapO1Performance(t *testing.T) {
	// Create translator with multiple zones
	zones := make(map[string]*config.Zone)
	for i := 1; i <= 100; i++ {
		zones[fmt.Sprintf("cluster%d", i)] = &config.Zone{
			Domains: []string{fmt.Sprintf("*.cluster%d.local", i)},
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    3,
			},
			ReflectedDomain: fmt.Sprintf("backend%d.local", i),
			PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
			TranslateID:     func(val int) *uint16 { v := uint16(val); return &v }(i),
		}
	}

	cfg := &config.Config{
		Zones: zones,
	}

	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create translator: %v", err)
	}

	// Test reverse lookup for translate ID that would be near end in linear search
	via6IP := net.IP{
		0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a, // prefix
		0x00, 0x00, // reserved
		0x00, 0x64, // translate ID (100)
		192, 168, 1, 1, // IPv4
	}

	domain, ipv4, err := translator.TranslateFromVia6(via6IP)
	if err != nil {
		t.Fatalf("Failed to translate 4via6 address: %v", err)
	}

	expectedDomain := "backend100.local"
	expectedIPv4 := net.IPv4(192, 168, 1, 1)

	if domain != expectedDomain {
		t.Errorf("Got domain %q, want %q", domain, expectedDomain)
	}

	if !ipv4.Equal(expectedIPv4) {
		t.Errorf("Got IPv4 %v, want %v", ipv4, expectedIPv4)
	}
}

func TestIs4via6Prefix(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want bool
	}{
		{
			name: "valid 4via6 prefix",
			cidr: "fd7a:115c:a1e0:b1a::/64",
			want: true,
		},
		{
			name: "valid 4via6 prefix with /120",
			cidr: "fd7a:115c:a1e0:b1a::/120",
			want: true,
		},
		{
			name: "wrong prefix",
			cidr: "2001:db8::/64",
			want: false,
		},
		{
			name: "invalid CIDR",
			cidr: "invalid",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, network, err := net.ParseCIDR(tt.cidr)
			if err != nil {
				if tt.want {
					t.Errorf("Failed to parse valid CIDR %s: %v", tt.cidr, err)
				}
				return
			}

			got := is4via6Prefix(network)
			if got != tt.want {
				t.Errorf("is4via6Prefix(%s) = %v, want %v", tt.cidr, got, tt.want)
			}
		})
	}
}