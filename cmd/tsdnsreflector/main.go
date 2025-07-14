// Package main provides the entry point for the tsdnsreflector service
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/dns"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

func main() {
	var configFile = flag.String("config", "./config.hujson", "Path to configuration file")
	var dryRun = flag.Bool("dry-run", false, "Only validate configuration and exit")
	
	// Initialize runtime configuration (defines additional flags)
	runtimeCfg := config.NewRuntimeConfig()
	
	flag.Parse()
	
	// Complete runtime config setup after flag parsing
	runtimeCfg.SetupEnvOnlyValues()

	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration from %s: %v\n", *configFile, err)
		os.Exit(1)
	}

	// Use runtime config for logging
	loggingCfg := runtimeCfg.ToLoggingConfig()
	log := logger.New(loggingCfg)

	// Count zones with 4via6 configured
	via6Zones := 0
	for _, zone := range cfg.Zones {
		if zone.Has4via6() {
			via6Zones++
		}
	}

	log.Info("Configuration loaded successfully",
		"file", *configFile,
		"dnsPort", runtimeCfg.DNSPort,
		"totalZones", len(cfg.Zones),
		"via6Zones", via6Zones,
		"globalBackendServers", len(cfg.Global.Backend.DNSServers),
		"logLevel", runtimeCfg.LogLevel,
		"logFormat", runtimeCfg.LogFormat)

	if *dryRun {
		log.Info("Configuration validation successful - exiting (dry-run mode)")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pass both configs to DNS server
	server, err := dns.NewServerWithRuntime(cfg, runtimeCfg)
	if err != nil {
		log.Error("Failed to create DNS server", "error", err)
		cancel()
		return
	}

	var metricsServer *http.Server
	if runtimeCfg.MetricsEnabled {
		serverCfg := runtimeCfg.ToServerConfig()
		metricsServer = startMetricsServer(&serverCfg)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		if err := server.Start(ctx); err != nil {
			log.Error("DNS server error", "error", err)
		}
	}()

	log.Info("tsdnsreflector started", "dnsPort", runtimeCfg.DNSPort)
	if runtimeCfg.MetricsEnabled {
		log.Info("Metrics server enabled", "port", runtimeCfg.HTTPPort, "path", runtimeCfg.MetricsPath)
	}

	for sig := range sigChan {
		switch sig {
		case syscall.SIGHUP:
			log.Info("Received SIGHUP, reloading configuration")
			// Note: Runtime config (env vars/flags) cannot be reloaded, only zone config
			if err := reloadConfiguration(server, *configFile); err != nil {
				log.Error("Configuration reload failed", "error", err)
			} else {
				log.Info("Configuration reloaded successfully (zones only)")
			}
		case syscall.SIGINT, syscall.SIGTERM:
			log.Info("Shutting down", "signal", sig.String())

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)

			server.Stop()

			if metricsServer != nil {
				if err := metricsServer.Shutdown(shutdownCtx); err != nil {
					log.Error("Metrics server shutdown error", "error", err)
				}
			}
			shutdownCancel()
			return
		}
	}
}

func startMetricsServer(cfg *config.ServerConfig) *http.Server {
	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.Handler())

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<html>
<head><title>tsdnsreflector Metrics</title></head>
<body>
<h1>tsdnsreflector Metrics</h1>
<p><a href="%s">Metrics</a></p>
</body>
</html>`, cfg.MetricsPath)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Default().Error("Metrics server error", "error", err)
		}
	}()

	return server
}

func reloadConfiguration(server *dns.Server, configFile string) error {
	newCfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load new configuration: %w", err)
	}

	// Log zone configuration changes for debugging
	newZoneCount := 0

	for name, zone := range newCfg.Zones {
		// Zone is enabled by being present in configuration
		newZoneCount++
		if zone.Has4via6() {
			logger.Default().Debug("Zone configured with 4via6",
				"zone", name,
				"domains", zone.Domains,
				"reflectedDomain", zone.GetReflectedDomain(),
				"translateID", zone.GetTranslateID())
		}
	}

	logger.Default().Info("Reloading configuration",
		"file", configFile,
		"totalZones", len(newCfg.Zones),
		"enabledZones", newZoneCount)

	if err := server.ReloadConfig(newCfg); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	return nil
}
