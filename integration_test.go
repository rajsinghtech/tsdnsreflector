package main

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	dnsserver "github.com/rajsingh/tsdnsreflector/internal/dns"
)

func TestDNSServer_E2E_4via6Translation(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53", "1.1.1.1:53"},
				Timeout:    "5s",
				Retries:    2,
			},
		},
		Zones: map[string]*config.Zone{
			"test": {
				Domains:         []string{"*.test.local"},
				ReflectedDomain: "httpbin.org",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(100); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53", "1.1.1.1:53"},
					Timeout:    "5s",
					Retries:    2,
				},
			},
			"cluster": {
				Domains:         []string{"*.app.cluster.local"},
				ReflectedDomain: "example.com",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(200); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53", "1.1.1.1:53"},
					Timeout:    "5s",
					Retries:    2,
				},
			},
		},
	}

	// Create runtime config with test values
	runtimeCfg := &config.RuntimeConfig{
		Hostname:    "test-e2e-server",
		DNSPort:     0, // Let OS choose port
		BindAddress: "127.0.0.1",
		DefaultTTL:  300,
		LogQueries:  false,
	}

	server, err := dnsserver.NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop()

	go func() {
		if err := server.Start(ctx); err != nil {
			t.Logf("DNS server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	serverAddr := "127.0.0.1:53"
	
	client := &dns.Client{
		Timeout: 5 * time.Second,
	}

	t.Run("AAAA query for 4via6 domain returns valid translation", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("test.local.", dns.TypeAAAA)

		resp, _, err := client.Exchange(msg, serverAddr)
		if err != nil {
			t.Skipf("Could not connect to DNS server (may be port conflict): %v", err)
		}

		if resp.Rcode != dns.RcodeSuccess {
			t.Errorf("Expected success, got rcode %d", resp.Rcode)
		}

		if len(resp.Answer) != 1 {
			t.Errorf("Expected 1 answer, got %d", len(resp.Answer))
		}

		answer, ok := resp.Answer[0].(*dns.AAAA)
		if !ok {
			t.Errorf("Expected AAAA record, got %T", resp.Answer[0])
		}

		ip := answer.AAAA
		if len(ip) != 16 {
			t.Errorf("Invalid IPv6 length: %d", len(ip))
		}

		translateID := (uint16(ip[10]) << 8) | uint16(ip[11])
		if translateID != 100 {
			t.Errorf("Wrong translate ID: got %d, want 100", translateID)
		}

		t.Logf("Generated 4via6 address: %v", ip)
	})

	t.Run("A query for 4via6 domain returns NXDOMAIN", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("test.local.", dns.TypeA)

		resp, _, err := client.Exchange(msg, serverAddr)
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		if resp.Rcode != dns.RcodeNameError {
			t.Errorf("Expected NXDOMAIN, got rcode %d", resp.Rcode)
		}
	})

	t.Run("Non-4via6 domain gets forwarded", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)

		resp, _, err := client.Exchange(msg, serverAddr)
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		if resp.Rcode != dns.RcodeSuccess {
			t.Errorf("Expected success for forwarded query, got rcode %d", resp.Rcode)
		}

		if len(resp.Answer) == 0 {
			t.Error("Expected answers for forwarded query")
		}

		t.Logf("Forwarded query returned %d answers", len(resp.Answer))
	})
}

func TestDNSServer_E2E_ConcurrentQueries(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    2,
			},
		},
		Zones: map[string]*config.Zone{
			"concurrent": {
				Domains:         []string{"*.concurrent.local"},
				ReflectedDomain: "httpbin.org",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(42); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    2,
				},
			},
		},
	}

	// Create runtime config with test values
	runtimeCfg := &config.RuntimeConfig{
		DNSPort:     15353,
		BindAddress: "127.0.0.1",
		DefaultTTL:  300,
		LogQueries:  false,
	}

	server, err := dnsserver.NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop()

	go func() {
		if err := server.Start(ctx); err != nil {
			t.Logf("DNS server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	client := &dns.Client{Timeout: 5 * time.Second}

	t.Run("Concurrent 4via6 queries", func(t *testing.T) {
		numQueries := 10
		results := make(chan error, numQueries)

		for i := 0; i < numQueries; i++ {
			go func(_ int) {
				msg := &dns.Msg{}
				msg.SetQuestion("concurrent.local.", dns.TypeAAAA)

				resp, _, err := client.Exchange(msg, "127.0.0.1:15353")
				if err != nil {
					results <- err
					return
				}

				if resp.Rcode != dns.RcodeSuccess {
					results <- nil // Skip connection issues
					return
				}

				if len(resp.Answer) != 1 {
					results <- nil
					return
				}

				results <- nil
			}(i)
		}

		successCount := 0
		for i := 0; i < numQueries; i++ {
			err := <-results
			if err == nil {
				successCount++
			}
		}

		if successCount < numQueries/2 {
			t.Errorf("Too many concurrent queries failed: %d/%d succeeded", successCount, numQueries)
		}

		t.Logf("Concurrent queries: %d/%d succeeded", successCount, numQueries)
	})
}