// Package via6 provides IPv4-to-IPv6 translation functionality
package via6

import (
	"net"
	"testing"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

// CreateTestTranslator creates a translator for testing
func CreateTestTranslator(t *testing.T, domains []string, reflectedDomain string, translateID uint16) *Translator {
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
				Domains: domains,
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    3,
				},
				ReflectedDomain: reflectedDomain,
				TranslateID:     &translateID,
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
			},
		},
	}
	translator, err := NewTranslator(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create test translator: %v", err)
	}
	return translator
}

// Create4via6Address creates a 4via6 IPv6 address from translate ID and IPv4
func Create4via6Address(translateID uint16, ipv4 net.IP) net.IP {
	via6 := make(net.IP, 16)

	copy(via6[:8], []byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a})
	via6[8] = 0x00
	via6[9] = 0x00

	via6[10] = byte(translateID >> 8)
	via6[11] = byte(translateID)

	ipv4Bytes := ipv4.To4()
	if ipv4Bytes != nil {
		copy(via6[12:], ipv4Bytes)
	}

	return via6
}

// Validate4via6Address validates a 4via6 address has expected components
func Validate4via6Address(t *testing.T, ip net.IP, expectedTranslateID uint16, expectedIPv4 net.IP) {
	if len(ip) != 16 {
		t.Errorf("4via6 address should be 16 bytes, got %d", len(ip))
		return
	}

	actualTranslateID := (uint16(ip[10]) << 8) | uint16(ip[11])
	if actualTranslateID != expectedTranslateID {
		t.Errorf("Wrong translate ID: got %d, want %d", actualTranslateID, expectedTranslateID)
	}

	if expectedIPv4 != nil {
		actualIPv4 := net.IP(ip[12:16])
		if !actualIPv4.Equal(expectedIPv4.To4()) {
			t.Errorf("Wrong IPv4 part: got %v, want %v", actualIPv4, expectedIPv4)
		}
	}
}
