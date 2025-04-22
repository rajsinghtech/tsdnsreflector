//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// convertTo4via6 converts an IPv4 address to a Tailscale 4via6 IPv6 address
// Format: fd7a:115c:a1e0:b1a:0:XXXX:YYYY:YYYY
// where XXXX is the site ID and YYYY:YYYY is the IPv4 in hex
func convertTo4via6Test(ipv4 net.IP, siteID int) net.IP {
	// Fixed prefix for Tailscale 4via6
	prefix := []byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a, 0x00, 0x00}

	// Site ID (two bytes)
	siteIDBytes := []byte{byte(siteID >> 8), byte(siteID)}

	// IPv4 address
	ipv4Bytes := ipv4.To4()

	// Create IPv6 address
	ipv6 := append(prefix, siteIDBytes...)
	ipv6 = append(ipv6, ipv4Bytes...)

	return net.IP(ipv6)
}

func TestConvertTo4via6(t *testing.T) {
	// Test the conversion function
	ipv4 := net.ParseIP("10.1.1.16").To4()
	siteID := 7

	ipv6 := convertTo4via6Test(ipv4, siteID)

	// Expected result: fd7a:115c:a1e0:b1a:0:7:a01:110
	expected := "fd7a:115c:a1e0:b1a:0:7:a01:110"
	if ipv6.String() != expected {
		t.Errorf("Expected %s, got %s", expected, ipv6.String())
	}
}

func TestDomainReplacement(t *testing.T) {
	// Test domain name replacement
	reflectedDomain := "cluster1.local."
	originalDomain := "cluster.local."

	testName := "example.default.svc.cluster1.local."
	expected := "example.default.svc.cluster.local."

	// Simple string replacement
	result := strings.Replace(testName, reflectedDomain, originalDomain, 1)

	fmt.Println("Original:", testName)
	fmt.Println("Expected:", expected)
	fmt.Println("Result:", result)

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}
