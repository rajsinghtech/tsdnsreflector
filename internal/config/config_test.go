package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func parseTimeout(timeoutStr string) time.Duration {
	timeout, err := ParseTimeout(timeoutStr)
	if err != nil {
		return 5 * time.Second
	}
	return timeout
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError bool
		validate  func(*Config) error
	}{
		{
			name: "valid config with all fields",
			content: `{
				"global": {
					"backend": {
						"dnsServers": ["1.1.1.1:53"],
						"timeout": "10s",
						"retries": 5
					},
					"cache": {
						"maxSize": 5000,
						"ttl": "600s"
					}
				},
				"zones": {
					"test": {
						"domains": ["*.test.local"],
						"backend": {
							"dnsServers": ["10.0.0.1:53"],
							"timeout": "5s",
							"retries": 3
						},
						"reflectedDomain": "real.local",
						"translateid": 42,
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64"
					}
				}
			}`,
			wantError: false,
			validate: func(cfg *Config) error {
				if parseTimeout(cfg.Global.Backend.Timeout) != 10*time.Second {
					t.Errorf("Expected timeout 10s, got %v", parseTimeout(cfg.Global.Backend.Timeout))
				}
				if cfg.Global.Cache.MaxSize != 5000 {
					t.Errorf("Expected cache maxSize 5000, got %d", cfg.Global.Cache.MaxSize)
				}
				if len(cfg.Zones) != 1 {
					t.Errorf("Expected 1 zone, got %d", len(cfg.Zones))
				}
				if zone, ok := cfg.Zones["test"]; ok {
					if zone.TranslateID == nil || *zone.TranslateID != 42 {
						t.Errorf("Expected zone with translateID 42, got %+v", zone.TranslateID)
					}
				} else {
					t.Error("Expected zone 'test' not found")
				}
				return nil
			},
		},
		{
			name: "minimal config with defaults",
			content: `{
				"zones": {
					"cluster": {
						
						"domains": ["*.cluster.local"],
						"backend": {
							"dnsServers": ["10.0.0.1:53"]
						},
						"4via6": {
							"reflectedDomain": "backend.local",
							"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
							"translateid": 1,
							
						}
					}
				}
			}`,
			wantError: false,
			validate: func(cfg *Config) error {
				if len(cfg.Global.Backend.DNSServers) != 2 {
					t.Errorf("Expected 2 default DNS servers, got %d", len(cfg.Global.Backend.DNSServers))
				}
				return nil
			},
		},
		{
			name: "HUJSON with comments",
			content: `{
				// Global configuration
				"global": {
					"backend": {
						"dnsServers": ["1.1.1.1:53"] // DNS server
					}
				},
				/* Block comment */
				"zones": {
					"test": {
						"domains": ["*.test.local"], // Domain pattern
						"backend": {
							"dnsServers": ["10.0.0.1:53"]
						}
					}
				} // Zone definition
			}`,
			wantError: false,
			validate: func(cfg *Config) error {
				if len(cfg.Zones) != 1 {
					t.Errorf("Expected 1 zone from HUJSON with comments")
				}
				return nil
			},
		},
		{
			name:      "invalid JSON",
			content:   `{"server": {invalid json}`,
			wantError: true,
		},
		{
			name: "invalid zone - no zones",
			content: `{
				"global": {
					"backend": {
						"dnsServers": ["1.1.1.1:53"]
					}
				},
				"zones": {}
			}`,
			wantError: true,
		},
		{
			name:      "empty file",
			content:   ``,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "config.hujson")

			err := os.WriteFile(configFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := Load(configFile)

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

			if tt.validate != nil {
				_ = tt.validate(cfg)
			}
		})
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/config.hujson")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestSetDefaults(t *testing.T) {
	cfg := &Config{}
	err := cfg.setDefaults()
	if err != nil {
		t.Fatalf("setDefaults failed: %v", err)
	}

	expectedServers := []string{"8.8.8.8:53", "1.1.1.1:53"}
	if len(cfg.Global.Backend.DNSServers) != len(expectedServers) {
		t.Errorf("Expected %d default DNS servers, got %d", len(expectedServers), len(cfg.Global.Backend.DNSServers))
	}
	if parseTimeout(cfg.Global.Backend.Timeout) != 5*time.Second {
		t.Errorf("Expected default timeout 5s, got %v", parseTimeout(cfg.Global.Backend.Timeout))
	}
	if cfg.Global.Backend.Retries != 3 {
		t.Errorf("Expected default retries 3, got %d", cfg.Global.Backend.Retries)
	}
	if cfg.Global.Cache.MaxSize != 10000 {
		t.Errorf("Expected default cache maxSize 10000, got %d", cfg.Global.Cache.MaxSize)
	}
	if cfg.Global.Cache.TTL != "300s" {
		t.Errorf("Expected default cache TTL '300s', got '%s'", cfg.Global.Cache.TTL)
	}
}

// Environment variable tests removed - now handled by RuntimeConfig

func TestZoneConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError bool
		validate  func(*Config) error
	}{
		{
			name: "valid zone with 4via6",
			content: `{
				"zones": {
					"test": {
						
						"domains": ["*.test.local"],
						"backend": {
							"dnsServers": ["10.0.0.1:53"]
						},
						"4via6": {
							"reflectedDomain": "backend.local",
							"translateid": 1,
							
						}
					}
				}
			}`,
			wantError: false,
			validate: func(cfg *Config) error {
				zone := cfg.GetZone("app.test.local")
				if zone == nil {
					t.Error("Expected zone for app.test.local")
				}
				return nil
			},
		},
		{
			name: "multiple zones with specificity",
			content: `{
				"zones": {
					"specific": {
						
						"domains": ["app.test.local"],
						"backend": {
							"dnsServers": ["10.0.0.1:53"]
						}
					},
					"wildcard": {
						
						"domains": ["*.test.local"],
						"backend": {
							"dnsServers": ["10.0.0.2:53"]
						}
					}
				}
			}`,
			wantError: false,
			validate: func(cfg *Config) error {
				// Specific match should win
				zone := cfg.GetZone("app.test.local")
				if zone == nil {
					t.Error("Expected specific zone to match")
				}
				// Wildcard should match other domains
				zone = cfg.GetZone("other.test.local")
				if zone == nil {
					t.Error("Expected wildcard zone to match")
				}
				return nil
			},
		},
		{
			name: "no zones configured",
			content: `{
				"zones": {}
			}`,
			wantError: true, // Should fail validation - no zones configured
			validate: func(cfg *Config) error {
				zone := cfg.GetZone("app.test.local")
				if zone != nil {
					t.Error("Expected no zone match when no zones configured")
				}
				return nil
			},
		},
		{
			name: "duplicate translateID",
			content: `{
				"zones": {
					"zone1": {
						"domains": ["*.zone1.local"],
						"backend": {
							"dnsServers": ["10.0.0.1:53"]
						},
						"reflectedDomain": "backend.local",
						"translateid": 1
					},
					"zone2": {
						"domains": ["*.zone2.local"],
						"backend": {
							"dnsServers": ["10.0.0.2:53"]
						},
						"reflectedDomain": "backend.local",
						"translateid": 1
					}
				}
			}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "test_config.hujson")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			cfg, err := Load(tmpFile)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
				return
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !tt.wantError && tt.validate != nil {
				_ = tt.validate(cfg)
			}
		})
	}
}

// OAuth and Tailscale configuration tests removed - now handled by RuntimeConfig
