package tailscale

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/kubestore"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
	"golang.org/x/oauth2/clientcredentials"
	"tailscale.com/client/local"
	"tailscale.com/client/tailscale" //nolint:staticcheck // v2 migration pending
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

// Sentinel errors for state store configuration
var (
	ErrStateStoreSkipped = errors.New("state store configuration skipped, using TSNet defaults")
)

type TSNetServer struct {
	server *tsnet.Server
	config *config.TailscaleConfig
	logger *logger.Logger
}

func NewTSNetServer(cfg *config.TailscaleConfig, appLogger *logger.Logger) (*TSNetServer, error) {
	ts := &TSNetServer{
		config: cfg,
		logger: appLogger,
	}

	// Resolve auth key from various sources (OAuth, environment, config)
	authKey, err := ts.resolveAuthKey(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve auth key: %w", err)
	}

	appLogger.Info("Creating TSNet server", "hostname", cfg.Hostname, "stateDir", cfg.StateDir)

	server := &tsnet.Server{
		Hostname: cfg.Hostname,
		AuthKey:  authKey,
		Dir:      cfg.StateDir,
		Logf:     log.Printf, // Use stdlib log for TSNet internal logs
	}

	ts.server = server

	// Configure state storage based on configuration
	stateStore, err := setupStateStore(cfg, appLogger)
	if err != nil && err != ErrStateStoreSkipped {
		appLogger.Warn("State store setup warning", "error", err)
		// Continue with default filesystem storage
	} else if stateStore != nil {
		if store, ok := stateStore.(ipn.StateStore); ok {
			server.Store = store
			appLogger.Info("Custom state storage configured successfully", "type", "kubernetes")
		} else {
			appLogger.Error("State store does not implement ipn.StateStore interface")
		}
	} else {
		appLogger.Info("Using filesystem state storage", "dir", cfg.StateDir)
	}

	return ts, nil
}

func (ts *TSNetServer) Start(ctx context.Context) error {
	ts.logger.Info("Starting TSNet server", "hostname", ts.config.Hostname)
	return ts.server.Start()
}

func (ts *TSNetServer) Close() error {
	if ts.server != nil {
		return ts.server.Close()
	}
	return nil
}

func (ts *TSNetServer) Listen(network, address string) (net.Listener, error) {
	return ts.server.Listen(network, address)
}

func (ts *TSNetServer) ListenPacket(network, address string) (net.PacketConn, error) {
	return ts.server.ListenPacket(network, address)
}

func (ts *TSNetServer) TailscaleIPs() (ipv4, ipv6 net.IP) {
	ipv4Addr, ipv6Addr := ts.server.TailscaleIPs()
	var ipv4IP, ipv6IP net.IP

	if ipv4Addr.IsValid() {
		ipv4IP = ipv4Addr.AsSlice()
	}
	if ipv6Addr.IsValid() {
		ipv6IP = ipv6Addr.AsSlice()
	}

	return ipv4IP, ipv6IP
}

func (ts *TSNetServer) LocalClient() (*local.Client, error) {
	return ts.server.LocalClient()
}

// Dial creates a connection through the Tailscale network, with subnet route support
func (ts *TSNetServer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	return ts.server.Dial(ctx, network, address)
}

// setupStateStore configures the appropriate state store based on configuration
func setupStateStore(cfg *config.TailscaleConfig, appLogger *logger.Logger) (interface{}, error) {
	// Check environment variable first (highest priority)
	if stateVar := os.Getenv("TS_STATE"); stateVar != "" {
		appLogger.Info("Using state storage from environment", "tsState", stateVar)
		if strings.HasPrefix(stateVar, "kube:") {
			return kubestore.NewFromConfig(func(format string, args ...interface{}) {
				appLogger.Debug(fmt.Sprintf(format, args...))
			}, stateVar)
		}
		// For other state types, let TSNet handle it with Dir
		return nil, ErrStateStoreSkipped
	}

	// Use configuration stateSecret if specified
	if stateVar := strings.TrimSpace(cfg.StateSecret); stateVar != "" {
		appLogger.Info("Using state storage from configuration", "stateSecret", stateVar)
		if strings.HasPrefix(stateVar, "kube:") {
			return kubestore.NewFromConfig(func(format string, args ...interface{}) {
				appLogger.Debug(fmt.Sprintf(format, args...))
			}, stateVar)
		}
		// For other state types, let TSNet handle it
		return nil, ErrStateStoreSkipped
	}

	// Use filesystem storage (default)
	appLogger.Debug("Using default filesystem state storage")
	return nil, ErrStateStoreSkipped
}

// readCredential reads a credential from direct value, file, or environment variable
func (ts *TSNetServer) readCredential(direct, file, envVar string) (string, error) {
	// Direct value has highest priority
	if direct != "" {
		return direct, nil
	}

	// File-based credential
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("failed to read credential file %s: %w", file, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Environment variable
	if envVar != "" {
		value := os.Getenv(envVar)
		if value != "" {
			return value, nil
		}
	}

	return "", fmt.Errorf("no credential found")
}

// resolveAuthKey resolves auth key from various sources
func (ts *TSNetServer) resolveAuthKey(ctx context.Context) (string, error) {
	// 1. Use explicit authkey if provided
	if ts.config.AuthKey != "" {
		// Check if it's an OAuth client secret format
		if strings.HasPrefix(ts.config.AuthKey, "tskey-client-") {
			return ts.generateAuthKeyFromOAuthSecret(ctx, ts.config.AuthKey)
		}
		return ts.config.AuthKey, nil
	}

	// 2. Check environment variables
	if authKey := os.Getenv("TS_AUTHKEY"); authKey != "" {
		if strings.HasPrefix(authKey, "tskey-client-") {
			return ts.generateAuthKeyFromOAuthSecret(ctx, authKey)
		}
		return authKey, nil
	}

	// 3. Use OAuth config to generate authkey
	if ts.config.OAuth != nil {
		return ts.generateAuthKeyFromOAuthConfig(ctx)
	}

	return "", fmt.Errorf("no authentication method configured (set TS_AUTHKEY, config authKey, or configure OAuth)")
}

// generateAuthKeyFromOAuthConfig generates an authkey using OAuth configuration
func (ts *TSNetServer) generateAuthKeyFromOAuthConfig(ctx context.Context) (string, error) {
	oauth := ts.config.OAuth

	clientID, err := ts.readCredential(oauth.ClientID, oauth.ClientIDFile, "TS_API_CLIENT_ID")
	if err != nil {
		return "", fmt.Errorf("failed to read OAuth client ID: %w", err)
	}

	clientSecret, err := ts.readCredential(oauth.ClientSecret, oauth.ClientSecretFile, "TS_API_CLIENT_SECRET")
	if err != nil {
		return "", fmt.Errorf("failed to read OAuth client secret: %w", err)
	}

	return ts.generateAuthKeyWithOAuth(ctx, clientID, clientSecret, oauth)
}

// generateAuthKeyFromOAuthSecret parses OAuth client secret with query parameters
func (ts *TSNetServer) generateAuthKeyFromOAuthSecret(ctx context.Context, clientSecret string) (string, error) {
	// Parse client secret with optional parameters:
	// tskey-client-xxxx[?ephemeral=false&preauthorized=BOOL&baseURL=...]

	parts := strings.Split(clientSecret, "?")
	actualSecret := parts[0]

	// Extract client ID from OAuth client secret format: tskey-client-{CLIENT_ID}-{SECRET_SUFFIX}
	// Based on Tailscale patterns, the client ID is embedded in the secret
	clientID := ""
	if strings.HasPrefix(actualSecret, "tskey-client-") {
		// Extract the part between "tskey-client-" and the first "-" after that
		remainder := strings.TrimPrefix(actualSecret, "tskey-client-")
		if dashIndex := strings.Index(remainder, "-"); dashIndex > 0 {
			clientID = remainder[:dashIndex]
		} else {
			// If no dash found, use the entire remainder as client ID
			clientID = remainder
		}
	}

	oauth := &config.OAuthConfig{
		ClientSecret:  actualSecret,
		BaseURL:       "https://login.tailscale.com",
		Ephemeral:     false,
		Preauthorized: true,
	}

	// Parse query parameters if present
	if len(parts) > 1 {
		values, err := url.ParseQuery(parts[1])
		if err != nil {
			return "", fmt.Errorf("failed to parse OAuth parameters: %w", err)
		}

		if ephemeral := values.Get("ephemeral"); ephemeral != "" {
			oauth.Ephemeral = ephemeral == "true"
		}
		if preauth := values.Get("preauthorized"); preauth != "" {
			oauth.Preauthorized = preauth == "true"
		}
		if baseURL := values.Get("baseURL"); baseURL != "" {
			oauth.BaseURL = baseURL
		}
		if tags := values.Get("tags"); tags != "" {
			oauth.Tags = strings.Split(tags, ",")
		}
	}

	return ts.generateAuthKeyWithOAuth(ctx, clientID, actualSecret, oauth)
}

// generateAuthKeyWithOAuth generates an authkey using OAuth credentials
func (ts *TSNetServer) generateAuthKeyWithOAuth(ctx context.Context, clientID, clientSecret string, oauth *config.OAuthConfig) (string, error) {
	ts.logger.Info("Generating authkey using OAuth credentials", "baseURL", oauth.BaseURL, "ephemeral", oauth.Ephemeral)

	credentials := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     oauth.BaseURL + "/api/v2/oauth/token",
	}

	tsClient := tailscale.NewClient("-", nil) //nolint:staticcheck // v2 migration pending
	tsClient.HTTPClient = credentials.Client(ctx)
	tsClient.BaseURL = oauth.BaseURL

	caps := tailscale.KeyCapabilities{
		Devices: tailscale.KeyDeviceCapabilities{
			Create: tailscale.KeyDeviceCreateCapabilities{
				Ephemeral:     oauth.Ephemeral,
				Preauthorized: oauth.Preauthorized,
				Tags:          oauth.Tags,
			},
		},
	}

	authkey, _, err := tsClient.CreateKey(ctx, caps)
	if err != nil {
		return "", fmt.Errorf("failed to create authkey via OAuth: %w", err)
	}

	ts.logger.Info("Successfully generated authkey via OAuth", "ephemeral", oauth.Ephemeral, "preauthorized", oauth.Preauthorized)
	return authkey, nil
}
