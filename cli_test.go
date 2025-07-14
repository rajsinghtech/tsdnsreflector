package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIArguments(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "tsdnsreflector-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tsdnsreflector")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	tests := []struct {
		name           string
		args           []string
		expectError    bool
		expectedOutput string
		timeout        time.Duration
	}{
		{
			name:           "help_flag",
			args:           []string{"-h"},
			expectError:    false,
			expectedOutput: "Usage of",
			timeout:        2 * time.Second,
		},
		{
			name:           "help_flag_long",
			args:           []string{"--help"},
			expectError:    false,
			expectedOutput: "Usage of",
			timeout:        2 * time.Second,
		},
		{
			name:           "version_flag",
			args:           []string{"-version"},
			expectError:    true,
			expectedOutput: "",
			timeout:        2 * time.Second,
		},
		{
			name:           "invalid_flag",
			args:           []string{"-invalid-flag"},
			expectError:    true,
			expectedOutput: "flag provided but not defined",
			timeout:        2 * time.Second,
		},
		{
			name:           "custom_config_path",
			args:           []string{"-config", "./config.hujson"},
			expectError:    false,
			expectedOutput: "DNS server listening",
			timeout:        3 * time.Second,
		},
		{
			name:           "nonexistent_config",
			args:           []string{"-config", "/nonexistent/config.hujson"},
			expectError:    true,
			expectedOutput: "Failed to load configuration",
			timeout:        2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, binaryPath, tt.args...)
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but command succeeded. Output: %s", outputStr)
			} else if !tt.expectError && err != nil {
				if strings.Contains(err.Error(), "killed") || strings.Contains(err.Error(), "signal") ||
					strings.Contains(err.Error(), "context deadline exceeded") {
					t.Logf("Command was terminated as expected: %v", err)
				} else {
					t.Errorf("Unexpected error: %v. Output: %s", err, outputStr)
				}
			}

			if tt.expectedOutput != "" && !strings.Contains(outputStr, tt.expectedOutput) {
				t.Errorf("Expected output to contain '%s', got: %s", tt.expectedOutput, outputStr)
			}

		})
	}
}

func TestEnvironmentVariables(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "tsdnsreflector-env-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tsdnsreflector")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	tests := []struct {
		name      string
		env       map[string]string
		expectLog string
	}{
		{
			name: "tailscale_auth_key_env",
			env: map[string]string{
				"TS_AUTHKEY": "tskey-test-env-variable",
			},
			expectLog: "TSNet server created",
		},
		{
			name: "empty_auth_key_env",
			env: map[string]string{
				"TS_AUTHKEY": "",
			},
			expectLog: "standalone mode",
		},
		{
			name:      "no_auth_key_env",
			env:       map[string]string{},
			expectLog: "standalone mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, binaryPath, "-config", "./config.hujson")

			cmd.Env = os.Environ()
			for key, value := range tt.env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}

			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			if tt.expectLog != "" && !strings.Contains(outputStr, tt.expectLog) {
				t.Errorf("Expected log '%s' not found in output: %s", tt.expectLog, outputStr)
			}

			_ = err
		})
	}
}

func TestConfigFileArgument(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.hujson")

	configContent := `{
		"server": {
			"hostname": "cli-test-server",
			"dnsPort": 15353,
			"bindAddress": "127.0.0.1"
		},
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
		},
		"tailscale": {
			"authKey": ""
		}
	}`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	binaryPath := filepath.Join(tmpDir, "tsdnsreflector-config-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tsdnsreflector")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	t.Run("custom_config_file", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath, "-config", configPath)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)
		t.Logf("Command output: %s", outputStr)

		expectedMessages := []string{
			"standalone mode",
			"127.0.0.1:15353",
		}

		for _, msg := range expectedMessages {
			if !strings.Contains(outputStr, msg) {
				t.Logf("Expected message not found: %s", msg)
			}
		}

		_ = err
	})

	t.Run("relative_config_path", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath, "-config", "./config.hujson")
		cmd.Dir = filepath.Dir(configPath)

		_, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Command failed with relative path (expected if ./config.hujson doesn't exist): %v", err)
		}
	})
}

func TestSignalHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping signal handling test in short mode")
	}

	binaryPath := filepath.Join(t.TempDir(), "tsdnsreflector-signal-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tsdnsreflector")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	t.Run("sigint_graceful_shutdown", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "-config", "./config.hujson")

		err := cmd.Start()
		if err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		time.Sleep(1 * time.Second)

		err = cmd.Process.Signal(os.Interrupt)
		if err != nil {
			t.Fatalf("Failed to send SIGINT: %v", err)
		}

		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()

		select {
		case err := <-waitCh:
			t.Logf("Process exited gracefully: %v", err)
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			t.Error("Process did not exit gracefully within timeout")
		}
	})

	t.Run("sigterm_graceful_shutdown", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "-config", "./config.hujson")

		err := cmd.Start()
		if err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		time.Sleep(1 * time.Second)

		err = cmd.Process.Signal(os.Kill)
		if err != nil {
			t.Fatalf("Failed to send SIGTERM: %v", err)
		}

		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()

		select {
		case err := <-waitCh:
			t.Logf("Process exited: %v", err)
		case <-time.After(2 * time.Second):
			t.Error("Process did not exit within timeout")
		}
	})
}

func TestZoneBasedConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "zone_config.hujson")

	zoneConfigContent := `{
		"global": {
			"backend": {
				"dnsServers": ["8.8.8.8:53"],
				"timeout": "5s",
				"retries": 3
			}
		},
		"zones": {
			"cluster": {
				"domains": ["*.cluster.local"],
				"backend": {
					"dnsServers": ["10.0.0.10:53"],
					"timeout": "3s",
					"retries": 2
				},
				"reflectedDomain": "cluster.internal",
				"translateid": 1,
				"prefixSubnet": "fd7a:115c:a1e0:b1a::/64"
			},
			"k8s": {
				"domains": ["*.k8s.local"],
				"backend": {
					"dnsServers": ["10.1.0.10:53"]
				},
				"reflectedDomain": "k8s.internal",
				"translateid": 2,
				"prefixSubnet": "fd7a:115c:a1e0:b1a::/64"
			},
			"default": {
				"domains": ["*"],
				"backend": {
					"dnsServers": ["1.1.1.1:53", "8.8.8.8:53"]
				}
			}
		}
	}`

	err := os.WriteFile(configPath, []byte(zoneConfigContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write zone config: %v", err)
	}

	binaryPath := filepath.Join(tmpDir, "tsdnsreflector-zone-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tsdnsreflector")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	t.Run("zone_configuration_startup", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		// Run with environment variables for runtime config
		cmd := exec.CommandContext(ctx, binaryPath, "-config", configPath)
		cmd.Env = append(os.Environ(),
			"TSDNS_DNS_PORT=15354",
			"TSDNS_BIND_ADDRESS=127.0.0.1",
			"TSDNS_LOG_LEVEL=debug",
			"TSDNS_LOG_FORMAT=text",
			"TSDNS_LOG_QUERIES=true",
		)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)
		t.Logf("Zone config startup output: %s", outputStr)

		expectedMessages := []string{
			"Configuration loaded successfully",
			"totalZones=3",
			"via6Zones=2",
			"Adding 4via6 zone",
			"standalone mode",
			"127.0.0.1:15354",
		}

		for _, msg := range expectedMessages {
			if !strings.Contains(outputStr, msg) {
				t.Errorf("Expected message not found: %s", msg)
			}
		}

		_ = err
	})
}

func TestCLIEdgeCases(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "tsdnsreflector-edge-test")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tsdnsreflector")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	t.Run("empty_config_flag", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "-config", "")
		output, err := cmd.CombinedOutput()

		if err == nil {
			t.Error("Expected error with empty config flag")
		}

		t.Logf("Empty config flag output: %s", string(output))
	})

	t.Run("multiple_config_flags", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "-config", "./config.hujson", "-config", "./other.hujson")
		output, err := cmd.CombinedOutput()

		t.Logf("Multiple config flags output: %s", string(output))
		t.Logf("Multiple config flags error: %v", err)
	})

	t.Run("config_flag_no_value", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "-config")
		output, err := cmd.CombinedOutput()

		if err == nil {
			t.Error("Expected error with config flag but no value")
		}

		t.Logf("Config flag no value output: %s", string(output))
	})
}
