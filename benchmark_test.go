package main

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	dnsserver "github.com/rajsingh/tsdnsreflector/internal/dns"
)

func BenchmarkDNSServer_4via6Translation(b *testing.B) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    1,
			},
		},
		Zones: map[string]*config.Zone{
			"bench": {
				Domains:         []string{"*.bench.local"},
				ReflectedDomain: "httpbin.org",
				PrefixSubnet:    "fd7a:115c:a1e0:b1a::/64",
				TranslateID:     func() *uint16 { v := uint16(42); return &v }(),
				Backend: config.BackendConfig{
					DNSServers: []string{"8.8.8.8:53"},
					Timeout:    "5s",
					Retries:    1,
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
		b.Fatalf("Failed to create DNS server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop()

	go func() {
		if err := server.Start(ctx); err != nil {
			b.Logf("DNS server error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	client := &dns.Client{Timeout: 2 * time.Second}
	serverAddr := "127.0.0.1:15353"

	b.ResetTimer()

	b.Run("AAAA_4via6_queries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			msg := &dns.Msg{}
			msg.SetQuestion("bench.local.", dns.TypeAAAA)
			
			_, _, err := client.Exchange(msg, serverAddr)
			if err != nil {
				b.Errorf("Query failed: %v", err)
			}
		}
	})
}

func BenchmarkDNSServer_BackendForwarding(b *testing.B) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Backend: config.BackendConfig{
				DNSServers: []string{"8.8.8.8:53"},
				Timeout:    "5s",
				Retries:    1,
			},
		},
	}

	// Create runtime config with test values
	runtimeCfg := &config.RuntimeConfig{
		DNSPort:     15354,
		BindAddress: "127.0.0.1",
		DefaultTTL:  300,
		LogQueries:  false,
	}

	server, err := dnsserver.NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		b.Fatalf("Failed to create DNS server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop()

	go func() {
		if err := server.Start(ctx); err != nil {
			b.Logf("DNS server error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	client := &dns.Client{Timeout: 2 * time.Second}
	serverAddr := "127.0.0.1:15354"

	b.ResetTimer()

	b.Run("A_backend_queries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			msg := &dns.Msg{}
			msg.SetQuestion("example.com.", dns.TypeA)
			
			_, _, err := client.Exchange(msg, serverAddr)
			if err != nil {
				b.Errorf("Query failed: %v", err)
			}
		}
	})
}