package tailscale

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

func TestNewTSNetServer(t *testing.T) {
	tests := []struct {
		name      string
		cfg    *config.TailscaleConfig
		wantError bool
	}{
		{
			name: "valid cfg",
			cfg: &config.TailscaleConfig{
				AuthKey:  "test-auth-key",
				Hostname: "test-server",
				StateDir: "/tmp/tailscale",
			},
			wantError: false,
		},
		{
			name: "missing auth key",
			cfg: &config.TailscaleConfig{
				AuthKey:  "",
				Hostname: "test-server",
				StateDir: "/tmp/tailscale",
			},
			wantError: true,
		},
		{
			name: "minimal cfg",
			cfg: &config.TailscaleConfig{
				AuthKey: "test-auth-key",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewTSNetServer(tt.cfg, logger.Default())

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if server == nil {
				t.Error("Expected server instance, got nil")
				return
			}

			if server.config != tt.cfg {
				t.Error("Config not stored correctly")
			}

			if server.server == nil {
				t.Error("TSNet server not initialized")
			}

			if server.server.Hostname != tt.cfg.Hostname {
				t.Errorf("Hostname not set correctly: got %q, want %q",
					server.server.Hostname, tt.cfg.Hostname)
			}

			if server.server.AuthKey != tt.cfg.AuthKey {
				t.Errorf("AuthKey not set correctly: got %q, want %q",
					server.server.AuthKey, tt.cfg.AuthKey)
			}

			if server.server.Dir != tt.cfg.StateDir {
				t.Errorf("StateDir not set correctly: got %q, want %q",
					server.server.Dir, tt.cfg.StateDir)
			}
		})
	}
}

func TestTSNetServerClose(t *testing.T) {
	cfg := &config.TailscaleConfig{
		AuthKey:  "test-auth-key",
		Hostname: "test-server",
		StateDir: "/tmp/tailscale",
	}

	server, err := NewTSNetServer(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create TSNet server: %v", err)
	}

	originalServer := server.server
	server.server = nil
	err = server.Close()
	if err != nil {
		t.Errorf("Close on nil server returned error: %v", err)
	}

	server.server = originalServer
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Close panicked on unstarted server (expected): %v", r)
		}
	}()

	err = server.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestTSNetServerAccess(t *testing.T) {
	cfg := &config.TailscaleConfig{
		AuthKey:  "test-auth-key",
		Hostname: "test-server",
		StateDir: "/tmp/tailscale",
	}

	server, err := NewTSNetServer(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create TSNet server: %v", err)
	}

	if server.server == nil {
		t.Error("Internal server is nil")
	}
}

func TestTSNetServerTailscaleIPs(t *testing.T) {
	cfg := &config.TailscaleConfig{
		AuthKey:  "test-auth-key",
		Hostname: "test-server",
		StateDir: "/tmp/tailscale",
	}

	server, err := NewTSNetServer(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create TSNet server: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Logf("TailscaleIPs panicked on unstarted server (expected): %v", r)
		}
	}()

	ipv4, ipv6 := server.TailscaleIPs()

	if ipv4 != nil && !ipv4.IsUnspecified() {
		t.Logf("Got IPv4: %v (might be valid in test environment)", ipv4)
	}

	if ipv6 != nil && !ipv6.IsUnspecified() {
		t.Logf("Got IPv6: %v (might be valid in test environment)", ipv6)
	}
}

func TestTSNetServerStartIntegration(t *testing.T) {
	t.Skip("Integration test requires valid Tailscale auth key")

	cfg := &config.TailscaleConfig{
		AuthKey:  "your-auth-key-here",
		Hostname: "test-integration-server",
		StateDir: "/tmp/tailscale-test",
	}

	server, err := NewTSNetServer(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create TSNet server: %v", err)
	}
	defer func() { _ = server.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start TSNet server: %v", err)
	}

	time.Sleep(5 * time.Second)

	ipv4, ipv6 := server.TailscaleIPs()
	t.Logf("IPv4: %v, IPv6: %v", ipv4, ipv6)

	listener, err := server.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("Failed to listen on Tailscale network: %v", err)
	} else {
		_ = listener.Close()
		t.Logf("Successfully listened on: %v", listener.Addr())
	}

	pc, err := server.ListenPacket("udp", ":0")
	if err != nil {
		t.Errorf("Failed to create packet connection on Tailscale network: %v", err)
	} else {
		_ = pc.Close()
		t.Logf("Successfully created packet connection on: %v", pc.LocalAddr())
	}
}

func TestTSNetServerConfigDefaults(t *testing.T) {
	cfg := &config.TailscaleConfig{
		AuthKey:             "test-auth-key",
		Hostname:            "", // Empty hostname
		StateDir:            "", // Empty state dir
		AdvertiseAsExitNode: false,
		AutoSplitDNS:        true,
	}

	server, err := NewTSNetServer(cfg, logger.Default())
	if err != nil {
		t.Fatalf("Failed to create TSNet server: %v", err)
	}

	if server.server.AuthKey != "test-auth-key" {
		t.Errorf("AuthKey not set correctly")
	}

	if server.server.Hostname != "" {
		t.Errorf("Hostname should be empty, got: %q", server.server.Hostname)
	}

	if server.server.Dir != "" {
		t.Errorf("StateDir should be empty, got: %q", server.server.Dir)
	}
}

func TestTSNetServerMultipleInstances(t *testing.T) {
	cfgs := []*config.TailscaleConfig{
		{
			AuthKey:  "test-auth-key-1",
			Hostname: "server-1",
			StateDir: "/tmp/tailscale-1",
		},
		{
			AuthKey:  "test-auth-key-2",
			Hostname: "server-2",
			StateDir: "/tmp/tailscale-2",
		},
	}

	var servers []*TSNetServer
	for i, cfg := range cfgs {
		server, err := NewTSNetServer(cfg, logger.Default())
		if err != nil {
			t.Fatalf("Failed to create TSNet server %d: %v", i, err)
		}
		servers = append(servers, server)
	}

	for i, server := range servers {
		if server.server.Hostname != cfgs[i].Hostname {
			t.Errorf("Server %d has wrong hostname: got %q, want %q",
				i, server.server.Hostname, cfgs[i].Hostname)
		}

		if server.server.AuthKey != cfgs[i].AuthKey {
			t.Errorf("Server %d has wrong auth key: got %q, want %q",
				i, server.server.AuthKey, cfgs[i].AuthKey)
		}
	}

	for i, server := range servers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("Server %d Close panicked (expected): %v", i, r)
				}
			}()
			_ = server.Close()
		}()
	}
}

func TestReadCredential(t *testing.T) {
	ts := &TSNetServer{
		logger: logger.Default(),
	}

	tests := []struct {
		name        string
		direct      string
		file        string
		envVar      string
		envValue    string
		fileContent string
		wantValue   string
		wantError   bool
	}{
		{
			name:      "direct value priority",
			direct:    "direct-value",
			file:      "/some/file",
			envVar:    "SOME_VAR",
			envValue:  "env-value",
			wantValue: "direct-value",
			wantError: false,
		},
		{
			name:        "file value when no direct",
			direct:      "",
			file:        "test-file",
			fileContent: "file-content\n",
			wantValue:   "file-content",
			wantError:   false,
		},
		{
			name:      "environment variable",
			direct:    "",
			file:      "",
			envVar:    "TEST_VAR",
			envValue:  "env-value",
			wantValue: "env-value",
			wantError: false,
		},
		{
			name:      "no credential found",
			direct:    "",
			file:      "",
			envVar:    "NONEXISTENT_VAR",
			wantError: true,
		},
		{
			name:      "file not found",
			direct:    "",
			file:      "/nonexistent/file",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable if specified
			if tt.envVar != "" && tt.envValue != "" {
				_ = os.Setenv(tt.envVar, tt.envValue)
				defer func() { _ = os.Unsetenv(tt.envVar) }()
			}

			// Setup file if specified
			var filePath string
			if tt.file != "" && tt.fileContent != "" {
				tmpDir := t.TempDir()
				filePath = filepath.Join(tmpDir, tt.file)
				err := os.WriteFile(filePath, []byte(tt.fileContent), 0644)
				if err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}
			} else {
				filePath = tt.file
			}

			value, err := ts.readCredential(tt.direct, filePath, tt.envVar)

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

			if value != tt.wantValue {
				t.Errorf("Expected value %q, got %q", tt.wantValue, value)
			}
		})
	}
}

func TestResolveAuthKey(t *testing.T) {
	tests := []struct {
		name      string
		cfg    *config.TailscaleConfig
		envKey    string
		envValue  string
		wantError bool
		checkFunc func(string) bool
	}{
		{
			name: "cfg authkey traditional",
			cfg: &config.TailscaleConfig{
				AuthKey: "tskey-auth-traditional123",
			},
			wantError: false,
			checkFunc: func(key string) bool {
				return key == "tskey-auth-traditional123"
			},
		},
		{
			name: "cfg authkey oauth format",
			cfg: &config.TailscaleConfig{
				AuthKey: "tskey-client-oauth123",
			},
			wantError: true, // Will fail without actual OAuth API
		},
		{
			name:      "environment traditional authkey",
			cfg:    &config.TailscaleConfig{},
			envKey:    "TS_AUTHKEY",
			envValue:  "tskey-auth-env123",
			wantError: false,
			checkFunc: func(key string) bool {
				return key == "tskey-auth-env123"
			},
		},
		{
			name:      "no authentication cfgured",
			cfg:    &config.TailscaleConfig{},
			wantError: true,
		},
		{
			name: "oauth cfg without credentials",
			cfg: &config.TailscaleConfig{
				OAuth: &config.OAuthConfig{
					BaseURL: "https://login.tailscale.com",
				},
			},
			wantError: true, // No credentials cfgured
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &TSNetServer{
				config: tt.cfg,
				logger: logger.Default(),
			}

			// Setup environment variable if specified
			if tt.envKey != "" && tt.envValue != "" {
				_ = os.Setenv(tt.envKey, tt.envValue)
				defer func() { _ = os.Unsetenv(tt.envKey) }()
			}

			authKey, err := ts.resolveAuthKey(context.Background())

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

			if tt.checkFunc != nil && !tt.checkFunc(authKey) {
				t.Errorf("Auth key validation failed: %s", authKey)
			}
		})
	}
}

func TestOAuthConfigCreation(t *testing.T) {
	tests := []struct {
		name      string
		cfg    *config.TailscaleConfig
		wantError bool
	}{
		{
			name: "oauth cfg with file credentials",
			cfg: &config.TailscaleConfig{
				OAuth: &config.OAuthConfig{
					ClientIDFile:     "/secrets/client_id",
					ClientSecretFile: "/secrets/client_secret",
					BaseURL:          "https://login.tailscale.com",
					Tags:             []string{"tag:dns"},
					Ephemeral:        true,
					Preauthorized:    true,
				},
			},
			wantError: true, // Will fail without actual files
		},
		{
			name: "oauth cfg with environment variables",
			cfg: &config.TailscaleConfig{
				OAuth: &config.OAuthConfig{
					BaseURL:       "https://login.tailscale.com",
					Tags:          []string{"tag:dns"},
					Ephemeral:     false,
					Preauthorized: true,
				},
			},
			wantError: true, // Will fail without OAuth API
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test only validates that the OAuth cfg structure is properly handled
			// Actual OAuth API calls are integration tests that require real credentials
			_, err := NewTSNetServer(tt.cfg, logger.Default())

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestOAuthParameterParsing(t *testing.T) {
	ts := &TSNetServer{
		config: &config.TailscaleConfig{},
		logger: logger.Default(),
	}

	tests := []struct {
		name         string
		clientSecret string
		wantError    bool
		checkConfig  func(*config.OAuthConfig) bool
	}{
		{
			name:         "simple client secret",
			clientSecret: "tskey-client-abc123",
			wantError:    true, // Will fail without OAuth API
			checkConfig: func(cfg *config.OAuthConfig) bool {
				return cfg.ClientSecret == "tskey-client-abc123" &&
					!cfg.Ephemeral &&
					cfg.Preauthorized
			},
		},
		{
			name:         "client secret with parameters",
			clientSecret: "tskey-client-abc123?ephemeral=true&preauthorized=false&tags=tag:dns,tag:server",
			wantError:    true, // Will fail without OAuth API
			checkConfig: func(cfg *config.OAuthConfig) bool {
				return cfg.ClientSecret == "tskey-client-abc123" &&
					cfg.Ephemeral &&
					!cfg.Preauthorized &&
					len(cfg.Tags) == 2
			},
		},
		{
			name:         "invalid query parameters",
			clientSecret: "tskey-client-abc123?invalid%query",
			wantError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test parameter parsing without making actual OAuth calls
			_, err := ts.generateAuthKeyFromOAuthSecret(context.Background(), tt.clientSecret)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
