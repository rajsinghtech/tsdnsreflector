package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/dns"
)

func TestConfigReload(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.hujson")

	initialConfig := `{
		"global": {
			"backend": {
				"dnsServers": ["8.8.8.8:53"],
				"timeout": "5s",
				"retries": 3
			}
		},
		"zones": {
			"test-zone": {
				"domains": ["*.cluster.local"],
				"backend": {
					"dnsServers": ["8.8.8.8:53"],
					"timeout": "5s",
					"retries": 3
				},
				"reflectedDomain": "cluster1.local",
				"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
				"translateid": 1
			}
		}
	}`

	if err := os.WriteFile(configFile, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to write initial config: %v", err)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("Failed to load initial config: %v", err)
	}

	// Create runtime config for test
	runtimeCfg := &config.RuntimeConfig{
		Hostname:   "test-server",
		DNSPort:    53,
		DefaultTTL: 300,
		LogLevel:   "info",
		LogFormat:  "json",
	}

	server, err := dns.NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}

	// Test initial configuration values
	if len(cfg.Global.Backend.DNSServers) != 1 || cfg.Global.Backend.DNSServers[0] != "8.8.8.8:53" {
		t.Errorf("Unexpected backend DNS servers: %v", cfg.Global.Backend.DNSServers)
	}
	if len(cfg.Zones) != 1 {
		t.Errorf("Expected 1 zone, got %d", len(cfg.Zones))
	}

	// Update configuration
	updatedConfig := `{
		"global": {
			"backend": {
				"dnsServers": ["1.1.1.1:53", "8.8.8.8:53"],
				"timeout": "10s",
				"retries": 5
			}
		},
		"zones": {
			"cluster-zone": {
				"domains": ["*.cluster.local"],
				"backend": {
					"dnsServers": ["1.1.1.1:53"],
					"timeout": "10s",
					"retries": 5
				},
				"reflectedDomain": "cluster1.local",
				"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
				"translateid": 1
			},
			"k8s-zone": {
				"domains": ["*.k8s.local"],
				"backend": {
					"dnsServers": ["8.8.8.8:53"],
					"timeout": "10s",
					"retries": 5
				},
				"reflectedDomain": "k8s1.local",
				"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
				"translateid": 2
			}
		}
	}`

	// Write updated config
	if err := os.WriteFile(configFile, []byte(updatedConfig), 0644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	// Small delay to ensure file is written
	time.Sleep(10 * time.Millisecond)

	// Reload configuration
	err = reloadConfiguration(server, configFile)
	if err != nil {
		t.Fatalf("Failed to reload configuration: %v", err)
	}

	// Test updated configuration values
	// Note: We need to access the server's config to verify the reload
	// This would require exposing the config or adding getter methods
}

func TestConfigReloadValidation(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.hujson")

	// Initial valid configuration
	validConfig := `{
		"global": {
			"backend": {
				"dnsServers": ["8.8.8.8:53"]
			}
		},
		"zones": {
			"default": {
				"domains": ["*"],
				"backend": {
					"dnsServers": ["8.8.8.8:53"]
				}
			}
		}
	}`

	// Write valid config
	if err := os.WriteFile(configFile, []byte(validConfig), 0644); err != nil {
		t.Fatalf("Failed to write valid config: %v", err)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("Failed to load initial config: %v", err)
	}

	// Create runtime config for test
	runtimeCfg := &config.RuntimeConfig{
		Hostname:    "test-server",
		DNSPort:     53,
		BindAddress: "0.0.0.0",
	}

	server, err := dns.NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}

	// Test case: Invalid zone configuration
	invalidConfig := `{
		"global": {
			"backend": {
				"dnsServers": ["8.8.8.8:53"]
			}
		},
		"zones": {
			"invalid-zone": {
				"domains": [],
				"backend": {
					"dnsServers": ["8.8.8.8:53"]
				}
			}
		}
	}`

	// Write invalid config
	if err := os.WriteFile(configFile, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	// Attempt to reload - should fail
	err = reloadConfiguration(server, configFile)
	if err == nil {
		t.Error("Expected error reloading invalid configuration, but got none")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

