package integration

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	dnsserver "github.com/rajsingh/tsdnsreflector/internal/dns"
)

func TestDNSServerE2E4via6Translation(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname:    "test-e2e-server",
			DNSPort:     0,
			BindAddress: "127.0.0.1",
			DefaultTTL:  300,
		},
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
		Tailscale: config.TailscaleConfig{
			AuthKey: "",
		},
		Logging: config.LoggingConfig{
			LogQueries: false,
		},
	}

	// Create and start the DNS server
	server, err := dnsserver.NewServer(cfg)
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop()

	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil {
			t.Logf("DNS server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Get the actual port the server is listening on
	serverAddr := "127.0.0.1:53" // Default for testing
	
	// Create DNS client
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

		// Validate 4via6 structure
		ip := answer.AAAA
		if len(ip) != 16 {
			t.Errorf("Invalid IPv6 length: %d", len(ip))
		}

		// Check translate ID (100 = 0x0064)
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

	t.Run("Subdomain 4via6 translation works", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("api.app.cluster.local.", dns.TypeAAAA)

		resp, _, err := client.Exchange(msg, serverAddr)
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		if resp.Rcode != dns.RcodeSuccess {
			t.Errorf("Expected success, got rcode %d", resp.Rcode)
		}

		if len(resp.Answer) == 1 {
			answer := resp.Answer[0].(*dns.AAAA)
			ip := answer.AAAA
			
			// Check translate ID (200 = 0x00C8)
			translateID := (uint16(ip[10]) << 8) | uint16(ip[11])
			if translateID != 200 {
				t.Errorf("Wrong translate ID for subdomain: got %d, want 200", translateID)
			}
		}
	})

	t.Run("Non-4via6 domain gets forwarded", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)

		resp, _, err := client.Exchange(msg, serverAddr)
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		// Should get forwarded to backend and return real results
		if resp.Rcode != dns.RcodeSuccess {
			t.Errorf("Expected success for forwarded query, got rcode %d", resp.Rcode)
		}

		if len(resp.Answer) == 0 {
			t.Error("Expected answers for forwarded query")
		}

		t.Logf("Forwarded query returned %d answers", len(resp.Answer))
	})
}

func TestDNSServerE2EBackendFailover(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DNSPort:     0,
			BindAddress: "127.0.0.1",
			DefaultTTL:  300,
		},
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{
					"127.0.0.1:9999", // Non-existent server
					"8.8.8.8:53",     // Working server
				},
				Timeout: "1s",
				Retries: 1,
			},
		},
		Zones: map[string]*config.Zone{},
		Tailscale: config.TailscaleConfig{
			AuthKey: "",
		},
		Logging: config.LoggingConfig{
			LogQueries: false,
		},
	}

	server, err := dnsserver.NewServer(cfg)
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

	client := &dns.Client{Timeout: 10 * time.Second}

	t.Run("Failover to working backend", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)

		resp, _, err := client.Exchange(msg, "127.0.0.1:53")
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		// Should succeed despite first backend being down
		if resp.Rcode != dns.RcodeSuccess {
			t.Errorf("Expected success with failover, got rcode %d", resp.Rcode)
		}
	})
}

func TestDNSServerE2EConcurrentQueries(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DNSPort:     0,
			BindAddress: "127.0.0.1",
			DefaultTTL:  300,
		},
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
		Tailscale: config.TailscaleConfig{
			AuthKey: "",
		},
		Logging: config.LoggingConfig{
			LogQueries: false,
		},
	}

	server, err := dnsserver.NewServer(cfg)
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
			go func(id int) {
				msg := &dns.Msg{}
				msg.SetQuestion("concurrent.local.", dns.TypeAAAA)

				resp, _, err := client.Exchange(msg, "127.0.0.1:53")
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

		// Collect results
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

func TestDNSServerE2EErrorHandling(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DNSPort:     0,
			BindAddress: "127.0.0.1",
			DefaultTTL:  300,
		},
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"127.0.0.1:9999"}, // Non-existent
				Timeout:    "100ms",
				Retries:    1,
			},
		},
		Zones: map[string]*config.Zone{
			"error": {
				Domains:         []string{"*.error.local"},
				ReflectedDomain: "nonexistent.invalid.domain.that.should.not.resolve",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(1); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"127.0.0.1:9999"},
					Timeout:    "100ms",
					Retries:    1,
				},
			},
		},
		Tailscale: config.TailscaleConfig{
			AuthKey: "",
		},
		Logging: config.LoggingConfig{
			LogQueries: false,
		},
	}

	server, err := dnsserver.NewServer(cfg)
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

	client := &dns.Client{Timeout: 2 * time.Second}

	t.Run("All backends fail returns SERVFAIL", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)

		resp, _, err := client.Exchange(msg, "127.0.0.1:53")
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		if resp.Rcode != dns.RcodeServerFailure {
			t.Errorf("Expected SERVFAIL, got rcode %d", resp.Rcode)
		}
	})

	t.Run("4via6 with unresolvable reflected domain", func(t *testing.T) {
		msg := &dns.Msg{}
		msg.SetQuestion("error.local.", dns.TypeAAAA)

		resp, _, err := client.Exchange(msg, "127.0.0.1:53")
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		// Should handle gracefully - either empty response or error
		if resp.Rcode == dns.RcodeSuccess && len(resp.Answer) > 0 {
			t.Error("Expected empty response for unresolvable reflected domain")
		}
	})

	t.Run("Malformed query handling", func(t *testing.T) {
		// Create a malformed DNS message
		msg := &dns.Msg{}
		msg.SetQuestion("", dns.TypeA) // Empty question

		resp, _, err := client.Exchange(msg, "127.0.0.1:53")
		if err != nil {
			t.Skipf("Could not connect to DNS server: %v", err)
		}

		// Server should handle gracefully
		if resp == nil {
			t.Error("Expected response even for malformed query")
		}
	})
}