package config

import (
	"flag"
	"os"
	"strconv"
	"strings"
)

// RuntimeConfig holds configuration from environment variables and flags
type RuntimeConfig struct {
	// Server configuration
	Hostname       string
	DNSPort        int
	HTTPPort       int
	BindAddress    string
	DefaultTTL     uint32
	HealthEnabled  bool
	HealthPath     string
	MetricsEnabled bool
	MetricsPath    string

	// Tailscale configuration
	TSAuthKey             string
	TSState               string
	TSHostname            string
	TSStateDir            string
	TSExitNode            bool
	TSAutoSplitDNS        bool
	TSOAuthURL            string
	TSOAuthTags           string
	TSOAuthEphemeral      bool
	TSOAuthPreauthorized  bool

	// OAuth configuration (following k8s-operator patterns)
	ClientIDFile     string
	ClientSecretFile string
	TSAPIClientID    string // Fallback if files not available
	TSAPIClientSecret string // Fallback if files not available

	// Logging configuration
	LogLevel      string
	LogFormat     string
	LogQueries    bool
	LogFile       string
	
	// Internal: used to handle flag parsing
	defaultTTLFlag *uint64
}

// defaultEnv returns the value of the named env var, or defaultVal if unset
func defaultEnv(name, defaultVal string) string {
	if val, ok := os.LookupEnv(name); ok {
		return val
	}
	return defaultVal
}

// defaultBool returns the boolean value of the named env var, or defaultVal if unset or not a bool
func defaultBool(name string, defaultVal bool) bool {
	v := os.Getenv(name)
	ret, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return ret
}

// defaultInt returns the integer value of the named env var, or defaultVal if unset or not an int
func defaultInt(name string, defaultVal int) int {
	v := os.Getenv(name)
	ret, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return ret
}

// defaultUint32 returns the uint32 value of the named env var, or defaultVal if unset or not a uint32
func defaultUint32(name string, defaultVal uint32) uint32 {
	v := os.Getenv(name)
	ret, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return defaultVal
	}
	return uint32(ret)
}

// NewRuntimeConfig creates RuntimeConfig from flags and environment variables
func NewRuntimeConfig() *RuntimeConfig {
	rc := &RuntimeConfig{}
	
	// Create a local variable for uint64 flag
	var defaultTTLUint64 uint64

	// Define flags (flags take precedence over env vars)
	flag.StringVar(&rc.Hostname, "hostname", defaultEnv("TSDNS_HOSTNAME", "tsdnsreflector"), 
		"Server hostname. Can also be set via TSDNS_HOSTNAME env var.")
	flag.IntVar(&rc.DNSPort, "dns-port", defaultInt("TSDNS_DNS_PORT", 53),
		"DNS port. Can also be set via TSDNS_DNS_PORT env var.")
	flag.IntVar(&rc.HTTPPort, "http-port", defaultInt("TSDNS_HTTP_PORT", 8080),
		"HTTP port for metrics/health. Can also be set via TSDNS_HTTP_PORT env var.")
	flag.StringVar(&rc.BindAddress, "bind-address", defaultEnv("TSDNS_BIND_ADDRESS", "0.0.0.0"),
		"Bind address. Can also be set via TSDNS_BIND_ADDRESS env var.")
	flag.Uint64Var(&defaultTTLUint64, "default-ttl", uint64(defaultUint32("TSDNS_DEFAULT_TTL", 300)),
		"Default TTL. Can also be set via TSDNS_DEFAULT_TTL env var.")
	flag.BoolVar(&rc.HealthEnabled, "health", defaultBool("TSDNS_HEALTH_ENABLED", true),
		"Enable health endpoint. Can also be set via TSDNS_HEALTH_ENABLED env var.")
	flag.StringVar(&rc.HealthPath, "health-path", defaultEnv("TSDNS_HEALTH_PATH", "/health"),
		"Health endpoint path. Can also be set via TSDNS_HEALTH_PATH env var.")
	flag.BoolVar(&rc.MetricsEnabled, "metrics", defaultBool("TSDNS_METRICS_ENABLED", true),
		"Enable metrics endpoint. Can also be set via TSDNS_METRICS_ENABLED env var.")
	flag.StringVar(&rc.MetricsPath, "metrics-path", defaultEnv("TSDNS_METRICS_PATH", "/metrics"),
		"Metrics endpoint path. Can also be set via TSDNS_METRICS_PATH env var.")

	// Logging flags
	flag.StringVar(&rc.LogLevel, "log-level", defaultEnv("TSDNS_LOG_LEVEL", "info"),
		"Log level (debug, info, warn, error). Can also be set via TSDNS_LOG_LEVEL env var.")
	flag.StringVar(&rc.LogFormat, "log-format", defaultEnv("TSDNS_LOG_FORMAT", "json"),
		"Log format (json or text). Can also be set via TSDNS_LOG_FORMAT env var.")
	flag.BoolVar(&rc.LogQueries, "log-queries", defaultBool("TSDNS_LOG_QUERIES", false),
		"Enable query logging. Can also be set via TSDNS_LOG_QUERIES env var.")
	flag.StringVar(&rc.LogFile, "log-file", defaultEnv("TSDNS_LOG_FILE", ""),
		"Log file path (stdout if empty). Can also be set via TSDNS_LOG_FILE env var.")

	// Set default TTL from env var for now - will be overridden after flag.Parse()
	rc.DefaultTTL = defaultUint32("TSDNS_DEFAULT_TTL", 300)
	rc.defaultTTLFlag = &defaultTTLUint64

	return rc
}

// SetupEnvOnlyValues sets values that are only available via environment variables
func (rc *RuntimeConfig) SetupEnvOnlyValues() {
	// Apply TTL from flag if it was parsed
	if rc.defaultTTLFlag != nil {
		rc.DefaultTTL = uint32(*rc.defaultTTLFlag)
	}
	
	// Tailscale standard environment variables
	rc.TSAuthKey = os.Getenv("TS_AUTHKEY")
	rc.TSState = os.Getenv("TS_STATE")

	// OAuth file paths (k8s-operator pattern)
	rc.ClientIDFile = os.Getenv("CLIENT_ID_FILE")
	rc.ClientSecretFile = os.Getenv("CLIENT_SECRET_FILE")
	
	// OAuth direct values (fallback)
	rc.TSAPIClientID = os.Getenv("TS_API_CLIENT_ID")
	rc.TSAPIClientSecret = os.Getenv("TS_API_CLIENT_SECRET")

	// Tailscale configuration
	rc.TSHostname = defaultEnv("TSDNS_TS_HOSTNAME", rc.Hostname)
	rc.TSStateDir = defaultEnv("TSDNS_TS_STATE_DIR", "/tmp/tailscale")
	rc.TSExitNode = defaultBool("TSDNS_TS_EXIT_NODE", false)
	rc.TSAutoSplitDNS = defaultBool("TSDNS_TS_AUTO_SPLIT_DNS", false)
	
	// OAuth configuration
	rc.TSOAuthURL = defaultEnv("TSDNS_TS_OAUTH_URL", "https://login.tailscale.com")
	rc.TSOAuthTags = defaultEnv("TSDNS_TS_OAUTH_TAGS", "tag:dns")
	rc.TSOAuthEphemeral = defaultBool("TSDNS_TS_OAUTH_EPHEMERAL", true)
	rc.TSOAuthPreauthorized = defaultBool("TSDNS_TS_OAUTH_PREAUTHORIZED", true)
}

// GetOAuthClientID returns the OAuth client ID from file or env var
func (rc *RuntimeConfig) GetOAuthClientID() (string, error) {
	if rc.ClientIDFile != "" {
		data, err := os.ReadFile(rc.ClientIDFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return rc.TSAPIClientID, nil
}

// GetOAuthClientSecret returns the OAuth client secret from file or env var
func (rc *RuntimeConfig) GetOAuthClientSecret() (string, error) {
	if rc.ClientSecretFile != "" {
		data, err := os.ReadFile(rc.ClientSecretFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return rc.TSAPIClientSecret, nil
}

// ToServerConfig converts RuntimeConfig to the old ServerConfig format for compatibility
func (rc *RuntimeConfig) ToServerConfig() ServerConfig {
	return ServerConfig{
		Hostname:       rc.Hostname,
		DNSPort:        rc.DNSPort,
		HTTPPort:       rc.HTTPPort,
		BindAddress:    rc.BindAddress,
		DefaultTTL:     rc.DefaultTTL,
		HealthEnabled:  rc.HealthEnabled,
		HealthPath:     rc.HealthPath,
		MetricsEnabled: rc.MetricsEnabled,
		MetricsPath:    rc.MetricsPath,
	}
}

// ToLoggingConfig converts RuntimeConfig to the old LoggingConfig format for compatibility
func (rc *RuntimeConfig) ToLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:      rc.LogLevel,
		Format:     rc.LogFormat,
		LogQueries: rc.LogQueries,
		LogFile:    rc.LogFile,
	}
}

// ToTailscaleConfig converts RuntimeConfig to the old TailscaleConfig format for compatibility
func (rc *RuntimeConfig) ToTailscaleConfig() TailscaleConfig {
	cfg := TailscaleConfig{
		AuthKey:             rc.TSAuthKey,
		Hostname:            rc.TSHostname,
		StateDir:            rc.TSStateDir,
		StateSecret:         rc.TSState,
		AdvertiseAsExitNode: rc.TSExitNode,
		AutoSplitDNS:        rc.TSAutoSplitDNS,
	}
	
	// Set OAuth config if any OAuth values are present
	if rc.ClientIDFile != "" || rc.ClientSecretFile != "" || rc.TSAPIClientID != "" || rc.TSAPIClientSecret != "" {
		tags := []string{}
		if rc.TSOAuthTags != "" {
			tags = strings.Split(rc.TSOAuthTags, ",")
		}
		
		cfg.OAuth = &OAuthConfig{
			ClientID:         rc.TSAPIClientID,
			ClientSecret:     rc.TSAPIClientSecret,
			ClientIDFile:     rc.ClientIDFile,
			ClientSecretFile: rc.ClientSecretFile,
			BaseURL:          rc.TSOAuthURL,
			Tags:             tags,
			Ephemeral:        rc.TSOAuthEphemeral,
			Preauthorized:    rc.TSOAuthPreauthorized,
		}
	}
	
	return cfg
}

// Temporary compatibility structs (will be removed later)
type ServerConfig struct {
	Hostname       string
	DNSPort        int
	HTTPPort       int
	BindAddress    string
	DefaultTTL     uint32
	HealthEnabled  bool
	HealthPath     string
	MetricsEnabled bool
	MetricsPath    string
}

type LoggingConfig struct {
	Level      string
	Format     string
	LogQueries bool
	LogFile    string
}

type TailscaleConfig struct {
	AuthKey             string
	Hostname            string
	StateDir            string
	StateSecret         string
	AdvertiseAsExitNode bool
	AutoSplitDNS        bool
	OAuth               *OAuthConfig
}

type OAuthConfig struct {
	ClientID         string
	ClientSecret     string
	ClientIDFile     string
	ClientSecretFile string
	BaseURL          string
	Tags             []string
	Ephemeral        bool
	Preauthorized    bool
}