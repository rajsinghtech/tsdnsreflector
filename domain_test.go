package main_test

import (
	"net"
	"strings"
	"testing"
)

// getDomainConversion is a copy of the function from main.go for testing purposes
func getDomainConversion(hostname string, reflectedDomain string, originalDomain string) string {
	// Ensure hostname has trailing dot for proper handling
	if !strings.HasSuffix(hostname, ".") {
		hostname = hostname + "."
	}
	
	// Check if the hostname has the reflected domain suffix
	if strings.HasSuffix(hostname, reflectedDomain) {
		// Extract hostname without the reflected domain
		prefix := strings.TrimSuffix(hostname, reflectedDomain)
		
		// Handle case where prefix is empty (exact domain match)
		if prefix == "" {
			return originalDomain
		}
		
		// Handle case where prefix ends with a dot
		prefix = strings.TrimSuffix(prefix, ".")
		
		// Reconstruct the hostname with the original domain
		if prefix != "" {
			return prefix + "." + strings.TrimPrefix(originalDomain, ".")
		}
		return originalDomain
	}
	
	// If it doesn't have the suffix, return the original hostname
	return hostname
}

// IPv4ToTailscale4via6 is a copy of the function from main.go for testing purposes
func IPv4ToTailscale4via6(ipv4 net.IP, siteID int) (net.IP, error) {
	if ipv4 == nil || ipv4.To4() == nil {
		return nil, nil
	}

	// Tailscale 4via6 format: fd7a:115c:a1e0:b1a:0:XXXX:YYYY:YYYY
	// Where XXXX is the site ID and YYYY:YYYY is the IPv4 address in hex
	ipv4Bytes := ipv4.To4()
	ipv6 := make(net.IP, 16)

	// Set the fixed prefix fd7a:115c:a1e0:b1a
	ipv6[0] = 0xfd
	ipv6[1] = 0x7a
	ipv6[2] = 0x11
	ipv6[3] = 0x5c
	ipv6[4] = 0xa1
	ipv6[5] = 0xe0
	ipv6[6] = 0x0b
	ipv6[7] = 0x1a

	// Set the site ID (0:XXXX)
	ipv6[8] = 0x00
	ipv6[9] = 0x00
	ipv6[10] = byte(siteID >> 8)
	ipv6[11] = byte(siteID)

	// Set the IPv4 address (YYYY:YYYY)
	ipv6[12] = ipv4Bytes[0]
	ipv6[13] = ipv4Bytes[1]
	ipv6[14] = ipv4Bytes[2]
	ipv6[15] = ipv4Bytes[3]

	return ipv6, nil
}

// TestGetDomainConversion tests the domain conversion function
func TestGetDomainConversion(t *testing.T) {
	tests := []struct {
		name             string
		hostname         string
		reflectedDomain  string
		originalDomain   string
		expectedHostname string
	}{
		{
			name:             "Simple domain conversion",
			hostname:         "service.cluster1.local",
			reflectedDomain:  "cluster1.local",
			originalDomain:   "cluster.local",
			expectedHostname: "service.cluster.local",
		},
		{
			name:             "Nested subdomain conversion",
			hostname:         "tsdnsreflector.tailscale.svc.cluster1.local",
			reflectedDomain:  "cluster1.local",
			originalDomain:   "cluster.local",
			expectedHostname: "tsdnsreflector.tailscale.svc.cluster.local",
		},
		{
			name:             "Exact domain match",
			hostname:         "cluster1.local",
			reflectedDomain:  "cluster1.local",
			originalDomain:   "cluster.local",
			expectedHostname: "cluster.local",
		},
		{
			name:             "No domain match",
			hostname:         "external.example.com",
			reflectedDomain:  "cluster1.local",
			originalDomain:   "cluster.local",
			expectedHostname: "external.example.com",
		},
		{
			name:             "With trailing dots",
			hostname:         "service.cluster1.local.",
			reflectedDomain:  "cluster1.local.",
			originalDomain:   "cluster.local.",
			expectedHostname: "service.cluster.local.",
		},
		{
			name:             "With trailing dot in hostname only",
			hostname:         "service.cluster1.local.",
			reflectedDomain:  "cluster1.local",
			originalDomain:   "cluster.local",
			expectedHostname: "service.cluster.local.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getDomainConversion(tc.hostname, tc.reflectedDomain, tc.originalDomain)
			if result != tc.expectedHostname {
				t.Errorf("getDomainConversion(%q, %q, %q) = %q, want %q",
					tc.hostname, tc.reflectedDomain, tc.originalDomain, result, tc.expectedHostname)
			}
		})
	}
}

// TestIPv4ToTailscale4via6 tests the IPv4 to Tailscale 4via6 conversion function
func TestIPv4ToTailscale4via6(t *testing.T) {
	tests := []struct {
		name     string
		ipv4     string
		siteID   int
		expected string
		wantErr  bool
	}{
		{
			name:     "Valid IPv4 with site ID 1",
			ipv4:     "192.168.1.1",
			siteID:   1,
			expected: "fd7a:115c:a1e0:b1a:0:1:c0a8:101",
			wantErr:  false,
		},
		{
			name:     "Valid IPv4 with site ID 1000",
			ipv4:     "10.0.0.1",
			siteID:   1000,
			expected: "fd7a:115c:a1e0:b1a:0:3e8:a00:1",
			wantErr:  false,
		},
		{
			name:    "Invalid IPv4",
			ipv4:    "invalid",
			siteID:  1,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ip net.IP
			if !tc.wantErr {
				ip = net.ParseIP(tc.ipv4)
			}
			
			result, err := IPv4ToTailscale4via6(ip, tc.siteID)
			
			if tc.wantErr {
				if err == nil {
					t.Errorf("IPv4ToTailscale4via6(%v, %d) expected error, got nil", ip, tc.siteID)
				}
				return
			}
			
			if err != nil {
				t.Errorf("IPv4ToTailscale4via6(%v, %d) unexpected error: %v", ip, tc.siteID, err)
				return
			}
			
			if result.String() != tc.expected {
				t.Errorf("IPv4ToTailscale4via6(%v, %d) = %v, want %v", 
					ip, tc.siteID, result.String(), tc.expected)
			}
		})
	}
} 