package config

import (
	"os"
	"testing"
)

func TestRuntimeConfig(t *testing.T) {
	// Save original env vars
	origEnvs := map[string]string{
		"TSDNS_HOSTNAME":    os.Getenv("TSDNS_HOSTNAME"),
		"TSDNS_DNS_PORT":    os.Getenv("TSDNS_DNS_PORT"),
		"TSDNS_LOG_LEVEL":   os.Getenv("TSDNS_LOG_LEVEL"),
		"TS_AUTHKEY":        os.Getenv("TS_AUTHKEY"),
		"CLIENT_ID_FILE":    os.Getenv("CLIENT_ID_FILE"),
		"TS_API_CLIENT_ID":  os.Getenv("TS_API_CLIENT_ID"),
	}
	
	// Restore env vars after test
	defer func() {
		for k, v := range origEnvs {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	// Set test environment variables
	_ = os.Setenv("TSDNS_HOSTNAME", "test-hostname")
	_ = os.Setenv("TSDNS_DNS_PORT", "5353")
	_ = os.Setenv("TSDNS_LOG_LEVEL", "debug")
	_ = os.Setenv("TS_AUTHKEY", "test-authkey")
	_ = os.Setenv("CLIENT_ID_FILE", "/test/client_id")
	_ = os.Setenv("TS_API_CLIENT_ID", "test-client-id")
	
	// Create a RuntimeConfig manually without using NewRuntimeConfig
	// to avoid flag redefinition in tests
	rc := &RuntimeConfig{}
	rc.SetupEnvOnlyValues()
	
	// Test environment variable reading
	if rc.TSAuthKey != "test-authkey" {
		t.Errorf("Expected TSAuthKey 'test-authkey', got '%s'", rc.TSAuthKey)
	}
	if rc.ClientIDFile != "/test/client_id" {
		t.Errorf("Expected ClientIDFile '/test/client_id', got '%s'", rc.ClientIDFile)
	}
	if rc.TSAPIClientID != "test-client-id" {
		t.Errorf("Expected TSAPIClientID 'test-client-id', got '%s'", rc.TSAPIClientID)
	}
}

func TestRuntimeConfigDefaults(t *testing.T) {
	// Clear relevant env vars
	envVars := []string{
		"TSDNS_HOSTNAME", "TSDNS_DNS_PORT", "TSDNS_HTTP_PORT",
		"TSDNS_BIND_ADDRESS", "TSDNS_DEFAULT_TTL", "TSDNS_LOG_LEVEL",
	}
	for _, v := range envVars {
		_ = os.Unsetenv(v)
	}

	// Test the default functions directly without creating RuntimeConfig
	// (to avoid flag redefinition issues in tests)
	if defaultEnv("TSDNS_HOSTNAME", "tsdnsreflector") != "tsdnsreflector" {
		t.Errorf("Expected default hostname 'tsdnsreflector'")
	}
	if defaultInt("TSDNS_DNS_PORT", 53) != 53 {
		t.Errorf("Expected default DNS port 53")
	}
	if defaultInt("TSDNS_HTTP_PORT", 8080) != 8080 {
		t.Errorf("Expected default HTTP port 8080")
	}
	if defaultEnv("TSDNS_BIND_ADDRESS", "0.0.0.0") != "0.0.0.0" {
		t.Errorf("Expected default bind address '0.0.0.0'")
	}
	if defaultUint32("TSDNS_DEFAULT_TTL", 300) != 300 {
		t.Errorf("Expected default TTL 300")
	}
	if defaultEnv("TSDNS_LOG_LEVEL", "info") != "info" {
		t.Errorf("Expected default log level 'info'")
	}
}

func TestToServerConfig(t *testing.T) {
	rc := &RuntimeConfig{
		Hostname:       "test-server",
		DNSPort:        5353,
		HTTPPort:       8081,
		BindAddress:    "127.0.0.1",
		DefaultTTL:     600,
		HealthEnabled:  true,
		HealthPath:     "/healthz",
		MetricsEnabled: false,
		MetricsPath:    "/stats",
	}
	
	sc := rc.ToServerConfig()
	
	if sc.Hostname != rc.Hostname {
		t.Errorf("Expected hostname '%s', got '%s'", rc.Hostname, sc.Hostname)
	}
	if sc.DNSPort != rc.DNSPort {
		t.Errorf("Expected DNS port %d, got %d", rc.DNSPort, sc.DNSPort)
	}
	if sc.HTTPPort != rc.HTTPPort {
		t.Errorf("Expected HTTP port %d, got %d", rc.HTTPPort, sc.HTTPPort)
	}
	if sc.BindAddress != rc.BindAddress {
		t.Errorf("Expected bind address '%s', got '%s'", rc.BindAddress, sc.BindAddress)
	}
	if sc.DefaultTTL != rc.DefaultTTL {
		t.Errorf("Expected TTL %d, got %d", rc.DefaultTTL, sc.DefaultTTL)
	}
}

func TestToLoggingConfig(t *testing.T) {
	rc := &RuntimeConfig{
		LogLevel:   "debug",
		LogFormat:  "text",
		LogQueries: true,
		LogFile:    "/var/log/test.log",
	}
	
	lc := rc.ToLoggingConfig()
	
	if lc.Level != rc.LogLevel {
		t.Errorf("Expected log level '%s', got '%s'", rc.LogLevel, lc.Level)
	}
	if lc.Format != rc.LogFormat {
		t.Errorf("Expected log format '%s', got '%s'", rc.LogFormat, lc.Format)
	}
	if lc.LogQueries != rc.LogQueries {
		t.Errorf("Expected log queries %v, got %v", rc.LogQueries, lc.LogQueries)
	}
	if lc.LogFile != rc.LogFile {
		t.Errorf("Expected log file '%s', got '%s'", rc.LogFile, lc.LogFile)
	}
}

func TestToTailscaleConfig(t *testing.T) {
	rc := &RuntimeConfig{
		TSAuthKey:            "test-auth",
		TSHostname:           "test-ts",
		TSStateDir:           "/var/lib/ts",
		TSState:              "kube:test-pod",
		TSExitNode:           true,
		TSAutoSplitDNS:       true,
		ClientIDFile:         "/etc/oauth/id",
		ClientSecretFile:     "/etc/oauth/secret",
		TSAPIClientID:        "fallback-id",
		TSAPIClientSecret:    "fallback-secret",
		TSOAuthURL:           "https://test.login",
		TSOAuthTags:          "tag:test,tag:dns",
		TSOAuthEphemeral:     false,
		TSOAuthPreauthorized: true,
	}
	
	tc := rc.ToTailscaleConfig()
	
	if tc.AuthKey != rc.TSAuthKey {
		t.Errorf("Expected auth key '%s', got '%s'", rc.TSAuthKey, tc.AuthKey)
	}
	if tc.Hostname != rc.TSHostname {
		t.Errorf("Expected hostname '%s', got '%s'", rc.TSHostname, tc.Hostname)
	}
	if tc.StateDir != rc.TSStateDir {
		t.Errorf("Expected state dir '%s', got '%s'", rc.TSStateDir, tc.StateDir)
	}
	
	// Check OAuth config
	if tc.OAuth == nil {
		t.Fatal("Expected OAuth config to be set")
	}
	if tc.OAuth.ClientIDFile != rc.ClientIDFile {
		t.Errorf("Expected client ID file '%s', got '%s'", rc.ClientIDFile, tc.OAuth.ClientIDFile)
	}
	if len(tc.OAuth.Tags) != 2 || tc.OAuth.Tags[0] != "tag:test" || tc.OAuth.Tags[1] != "tag:dns" {
		t.Errorf("Expected tags [tag:test, tag:dns], got %v", tc.OAuth.Tags)
	}
	if tc.OAuth.Ephemeral != rc.TSOAuthEphemeral {
		t.Errorf("Expected ephemeral %v, got %v", rc.TSOAuthEphemeral, tc.OAuth.Ephemeral)
	}
}

func TestOAuthConfigNotSetWhenEmpty(t *testing.T) {
	rc := &RuntimeConfig{
		TSAuthKey:  "test-auth",
		TSHostname: "test-ts",
		// No OAuth fields set
	}
	
	tc := rc.ToTailscaleConfig()
	
	if tc.OAuth != nil {
		t.Errorf("Expected OAuth config to be nil when no OAuth fields are set")
	}
}