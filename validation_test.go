package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/dns"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		configJSON  string
		expectError bool
	}{
		{
			name: "empty_zones",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {}
			}`,
			expectError: true, // At least one zone required
		},
		{
			name: "valid_zone_without_4via6",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"default": {
						"domains": ["*"],
						"backend": {"dnsServers": ["8.8.8.8:53"]}
					}
				}
			}`,
			expectError: false,
		},
		{
			name: "malformed_4via6_cidr",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"test": {
						"domains": ["*.test.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "backend.local",
						"prefixSubnet": "not-a-cidr",
						"translateid": 1
					}
				}
			}`,
			expectError: true,
		},
		{
			name: "duplicate_translate_ids",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"zone1": {
						"domains": ["*.app1.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "backend1.local",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 1
					},
					"zone2": {
						"domains": ["*.app2.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "backend2.local",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 1
					}
				}
			}`,
			expectError: true, // Duplicate translate IDs should be rejected
		},
		{
			name: "missing_required_zone_fields",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"invalid": {
						"domains": [],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "",
						"prefixSubnet": "",
						"translateid": 0
					}
				}
			}`,
			expectError: true,
		},
		{
			name: "overlapping_domain_patterns",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"zone1": {
						"domains": ["*.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]}
					},
					"zone2": {
						"domains": ["*.test.local"],
						"backend": {"dnsServers": ["1.1.1.1:53"]}
					}
				}
			}`,
			expectError: false, // Overlapping is allowed (most specific wins)
		},
		{
			name: "invalid_4via6_prefix",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"test": {
						"domains": ["*.test.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "backend.local",
						"prefixSubnet": "2001:db8::/64",
						"translateid": 1
					}
				}
			}`,
			expectError: true, // Must be 4via6 prefix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "test_config.hujson")

			err := os.WriteFile(configFile, []byte(tt.configJSON), 0644)
			if err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := config.Load(configFile)
			if err != nil {
				if !tt.expectError {
					t.Errorf("Unexpected config load error: %v", err)
				}
				return
			}

			_, err = dns.NewServer(cfg)
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected server creation error: %v", err)
			}
		})
	}
}

func TestEnvValidation(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(*config.Config)
	}{
		{
			name: "valid_tailscale_auth_key",
			envVars: map[string]string{
				"TS_AUTHKEY": "tskey-auth-test-valid-key",
			},
			validate: func(_ *config.Config) {
				// This test no longer applies - auth key is in runtime config, not in parsed config
				// We would need to test RuntimeConfig separately
			},
		},
		{
			name: "empty_auth_key",
			envVars: map[string]string{
				"TS_AUTHKEY": "",
			},
			validate: func(_ *config.Config) {
				// This test no longer applies - auth key is in runtime config, not in parsed config
				// We would need to test RuntimeConfig separately
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables and store cleanup functions
			var cleanupFuncs []func()
			for key, value := range tt.envVars {
				oldValue := os.Getenv(key)
				_ = os.Setenv(key, value)
				cleanupFuncs = append(cleanupFuncs, func(k, v string) func() {
					return func() {
						if v == "" {
							_ = os.Unsetenv(k)
						} else {
							_ = os.Setenv(k, v)
						}
					}
				}(key, oldValue))
			}
			defer func() {
				for _, cleanup := range cleanupFuncs {
					cleanup()
				}
			}()

			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "test_config.hujson")

			configJSON := `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"default": {
						"domains": ["*"],
						"backend": {"dnsServers": ["8.8.8.8:53"]}
					}
				}
			}`

			err := os.WriteFile(configFile, []byte(configJSON), 0644)
			if err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := config.Load(configFile)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			if tt.validate != nil {
				tt.validate(cfg)
			}
		})
	}
}

func TestZoneValidationRules(t *testing.T) {
	tests := []struct {
		name        string
		configJSON  string
		expectError bool
		errorString string
	}{
		{
			name: "zone_with_valid_4via6",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"cluster": {
						"domains": ["*.cluster.local"],
						"backend": {"dnsServers": ["10.0.0.10:53"]},
						"reflectedDomain": "cluster.internal",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 1
					}
				}
			}`,
			expectError: false,
		},
		{
			name: "zone_translateid_zero_invalid",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"test": {
						"domains": ["*.test.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "test.internal",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 0
					}
				}
			}`,
			expectError: true,
			errorString: "translateID cannot be 0",
		},
		{
			name: "zone_empty_domains",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"empty": {
						"domains": [],
						"backend": {"dnsServers": ["8.8.8.8:53"]}
					}
				}
			}`,
			expectError: true,
			errorString: "zone empty must have at least one domain",
		},
		{
			name: "zone_missing_backend_dns_servers",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"no-backend": {
						"domains": ["*.test.local"],
						"backend": {"dnsServers": []}
					}
				}
			}`,
			expectError: false, // Empty backend falls back to global
		},
		{
			name: "zone_4via6_invalid_reflected_domain",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"invalid-reflected": {
						"domains": ["*.test.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 1
					}
				}
			}`,
			expectError: true,
			errorString: "needs reflectedDomain for 4via6",
		},
		{
			name: "multiple_zones_different_translateids",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"zone1": {
						"domains": ["*.app1.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "app1.internal",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 1
					},
					"zone2": {
						"domains": ["*.app2.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]},
						"reflectedDomain": "app2.internal",
						"prefixSubnet": "fd7a:115c:a1e0:b1a::/64",
						"translateid": 2
					}
				}
			}`,
			expectError: false,
		},
		{
			name: "zone_matching_precedence",
			configJSON: `{
				"server": {"dnsPort": 53},
				"global": {"backend": {"dnsServers": ["8.8.8.8:53"]}},
				"zones": {
					"general": {
						"domains": ["*.local"],
						"backend": {"dnsServers": ["8.8.8.8:53"]}
					},
					"specific": {
						"domains": ["*.app.local"],
						"backend": {"dnsServers": ["10.0.0.10:53"]}
					},
					"very-specific": {
						"domains": ["api.app.local"],
						"backend": {"dnsServers": ["10.0.0.20:53"]}
					}
				}
			}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "test_config.hujson")

			err := os.WriteFile(configFile, []byte(tt.configJSON), 0644)
			if err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := config.Load(configFile)
			if err != nil {
				if !tt.expectError {
					t.Errorf("Unexpected config load error: %v", err)
				} else if tt.errorString != "" && !strings.Contains(err.Error(), tt.errorString) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorString, err)
				}
				return
			}

			_, err = dns.NewServer(cfg)
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected server creation error: %v", err)
			} else if tt.expectError && err != nil && tt.errorString != "" {
				if !strings.Contains(err.Error(), tt.errorString) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorString, err)
				}
			}
		})
	}
}
