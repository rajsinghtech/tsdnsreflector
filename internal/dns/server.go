package dns

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/miekg/dns"
	via6 "github.com/rajsingh/tsdnsreflector/internal/4via6"
	"github.com/rajsingh/tsdnsreflector/internal/cache"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
	"github.com/rajsingh/tsdnsreflector/internal/memory"
	"github.com/rajsingh/tsdnsreflector/internal/metrics"
	"github.com/rajsingh/tsdnsreflector/internal/tailscale"
	"tailscale.com/client/local"
)

type Server struct {
	config        *config.Config
	runtimeCfg    *config.RuntimeConfig
	dnsServer     *dns.Server
	httpServer    *http.Server
	via6Trans     *via6.Translator
	forwarder     *Forwarder
	tsnetServer   *tailscale.TSNetServer
	handler       *TailscaleDNSHandler
	zoneCaches    map[string]*cache.ZoneCache
	memoryMonitor *memory.Monitor
	logger        *logger.Logger
}

type Forwarder struct {
	backends    []string
	timeout     time.Duration
	retries     int
	logger      *logger.Logger
	tsnetServer *tailscale.TSNetServer // Optional TSNet server for subnet routing
}

func parseTimeout(timeoutStr string) time.Duration {
	if timeoutStr == "" {
		return 5 * time.Second
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return 5 * time.Second
	}
	return timeout
}

// NewServer creates a new DNS server (deprecated - use NewServerWithRuntime)
func NewServer(cfg *config.Config) (*Server, error) {
	// Create a runtime config with defaults for backward compatibility
	runtimeCfg := &config.RuntimeConfig{
		Hostname:       "tsdnsreflector",
		DNSPort:        53,
		HTTPPort:       8080,
		BindAddress:    "0.0.0.0",
		DefaultTTL:     300,
		HealthEnabled:  true,
		HealthPath:     "/health",
		MetricsEnabled: true,
		MetricsPath:    "/metrics",
		LogLevel:       "info",
		LogFormat:      "json",
	}
	return NewServerWithRuntime(cfg, runtimeCfg)
}

// NewServerWithRuntime creates a new DNS server with runtime configuration
func NewServerWithRuntime(cfg *config.Config, runtimeCfg *config.RuntimeConfig) (*Server, error) {
	loggingCfg := runtimeCfg.ToLoggingConfig()
	log := logger.New(loggingCfg)

	via6Trans, err := via6.NewTranslator(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create 4via6 translator: %w", err)
	}

	// Initially create forwarder without TSNet (will be updated later if TSNet is available)
	forwarder := NewForwarder(cfg.Global.Backend, log)

	// Initialize memory monitor
	memoryLimits := memory.Limits{
		MaxZoneCount:     100,             // Max 100 zones
		MaxTotalMemory:   500 * 1024 * 1024, // 500MB total
		MaxCachePerZone:  50 * 1024 * 1024,  // 50MB per zone cache
		MaxBufferPerZone: 10 * 1024 * 1024,  // 10MB per zone buffer
	}
	memoryMonitor := memory.NewMonitor(log, memoryLimits)

	// Initialize zone caches
	zoneCaches := make(map[string]*cache.ZoneCache)
	for zoneName, zone := range cfg.Zones {
		// Warn about external client access
		if zone.AllowExternalClients {
			log.ZoneWarn(zoneName, "Zone allows external (non-Tailscale) client access", "domains", zone.Domains)
		}
		
		// Register zone for memory monitoring
		if err := memoryMonitor.RegisterZone(zoneName); err != nil {
			log.ZoneWarn(zoneName, "Failed to register zone for memory monitoring", "error", err)
		}

		if zone.Cache != nil {
			maxSize := zone.Cache.MaxSize
			if maxSize == 0 {
				maxSize = cfg.Global.Cache.MaxSize
			}
			ttl, _ := config.ParseCacheTTL(zone.Cache.TTL)
			zoneCaches[zoneName] = cache.NewZoneCacheWithName(maxSize, ttl, zoneName)
			log.ZoneInfo(zoneName, "Zone cache initialized", "maxSize", maxSize, "ttl", ttl)
		}
	}

	handler := &TailscaleDNSHandler{
		config:        cfg,
		runtimeCfg:    runtimeCfg,
		via6Trans:     via6Trans,
		forwarder:     forwarder,
		tsnetServer:   nil,
		zoneCaches:    zoneCaches,
		memoryMonitor: memoryMonitor,
		logger:        log,
	}

	server := &Server{
		config:        cfg,
		runtimeCfg:    runtimeCfg,
		via6Trans:     via6Trans,
		forwarder:     forwarder,
		handler:       handler,
		zoneCaches:    zoneCaches,
		memoryMonitor: memoryMonitor,
		logger:        log,
	}

	// Check for Tailscale auth from runtime config
	tsCfg := runtimeCfg.ToTailscaleConfig()
	if tsCfg.AuthKey != "" {
		tsnetServer, err := tailscale.NewTSNetServer(&tsCfg, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create TSNet server: %w", err)
		}
		server.tsnetServer = tsnetServer
		log.Info("TSNet server created", "hostname", tsCfg.Hostname)
	} else {
		log.Info("No Tailscale auth key provided, running in standalone mode")
	}

	server.dnsServer = &dns.Server{
		Net:     "udp",
		Handler: handler,
	}

	// For standalone mode (no TSNet), set the address immediately
	if tsCfg.AuthKey == "" {
		bindAddr := fmt.Sprintf("%s:%d", runtimeCfg.BindAddress, runtimeCfg.DNSPort)
		server.dnsServer.Addr = bindAddr
	}
	if runtimeCfg.HealthEnabled || runtimeCfg.MetricsEnabled {
		mux := http.NewServeMux()

		if runtimeCfg.HealthEnabled {
			mux.HandleFunc(runtimeCfg.HealthPath, server.healthHandler)
		}

		if runtimeCfg.MetricsEnabled {
			mux.HandleFunc(runtimeCfg.MetricsPath, server.metricsHandler)
		}

		server.httpServer = &http.Server{
			Addr:    fmt.Sprintf("%s:%d", runtimeCfg.BindAddress, runtimeCfg.HTTPPort),
			Handler: mux,
		}
	}

	return server, nil
}

func (s *Server) Start(ctx context.Context) error {
	var err error

	if s.tsnetServer != nil {
		if err = s.tsnetServer.Start(ctx); err != nil {
			return fmt.Errorf("failed to start TSNet server: %w", err)
		}

		if handler, ok := s.dnsServer.Handler.(*TailscaleDNSHandler); ok {
			handler.tsnetServer = s.tsnetServer
			// Update forwarder with TSNet for subnet route support
			handler.forwarder.tsnetServer = s.tsnetServer
			s.logger.Info("TSNet subnet routing enabled for DNS forwarding")
		}
		s.logger.Info("Waiting for Tailscale network to be ready...")
		var ipv4, ipv6 net.IP
		for i := 0; i < 10; i++ {
			ipv4, ipv6 = s.tsnetServer.TailscaleIPs()
			if ipv4 != nil || ipv6 != nil {
				break
			}
			if i == 9 {
				return fmt.Errorf("no Tailscale IP addresses available")
			}
			time.Sleep(2 * time.Second)
		}

		metrics.UpdateTailscaleStatus(true)
		go s.updateTailscaleMetrics(ctx)
		
		// Start memory monitoring
		if s.memoryMonitor != nil {
			s.memoryMonitor.StartPeriodicCheck(30 * time.Second)
			s.logger.Info("Memory monitoring started", "checkInterval", "30s")
		}

		var bindAddr string
		if ipv4 != nil {
			bindAddr = fmt.Sprintf("%s:%d", ipv4.String(), s.runtimeCfg.DNSPort)
			s.logger.Info("Using Tailscale IP address", "ip", ipv4.String(), "type", "IPv4")
		} else {
			bindAddr = fmt.Sprintf("[%s]:%d", ipv6.String(), s.runtimeCfg.DNSPort)
			s.logger.Info("Using Tailscale IP address", "ip", ipv6.String(), "type", "IPv6")
		}

		pc, err := s.tsnetServer.ListenPacket("udp", bindAddr)
		if err != nil {
			return fmt.Errorf("failed to bind DNS server to Tailscale network: %w", err)
		}

		s.dnsServer.PacketConn = pc
		s.logger.Info("DNS server listening on Tailscale network", "address", bindAddr)

		// Also start regular DNS server for Kubernetes port forwarding
		regularAddr := fmt.Sprintf("%s:%d", s.runtimeCfg.BindAddress, s.runtimeCfg.DNSPort)
		go func() {
			regularPC, err := net.ListenPacket("udp", regularAddr)
			if err != nil {
				s.logger.Error("Failed to start regular DNS server", "error", err, "address", regularAddr)
				return
			}
			defer func() { _ = regularPC.Close() }()

			regularServer := &dns.Server{
				PacketConn: regularPC,
				Handler:    s.dnsServer.Handler,
			}
			s.logger.Info("Regular DNS server listening", "address", regularAddr)
			if err := regularServer.ActivateAndServe(); err != nil {
				s.logger.Error("Regular DNS server error", "error", err)
			}
		}()

	} else {
		// In standalone mode, address was already set in constructor
		s.logger.Info("DNS server listening", "address", s.dnsServer.Addr)
	}

	if s.httpServer != nil {
		go func() {
			s.logger.Info("HTTP server listening", "address", s.httpServer.Addr)
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Error("HTTP server error", "error", err)
			}
		}()
	}

	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	// Use different methods based on whether we have TSNet or standalone
	if s.tsnetServer != nil {
		// TSNet mode: PacketConn is set, use ActivateAndServe
		return s.dnsServer.ActivateAndServe()
	} else {
		// Standalone mode: Addr is set, use ListenAndServe
		return s.dnsServer.ListenAndServe()
	}
}

func (s *Server) Stop() {
	// Update Tailscale status metric
	metrics.UpdateTailscaleStatus(false)

	// Stop cache cleanup routines
	for _, cache := range s.zoneCaches {
		cache.Stop()
	}

	if s.dnsServer != nil {
		_ = s.dnsServer.Shutdown()
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(ctx)
	}
	if s.tsnetServer != nil {
		_ = s.tsnetServer.Close()
	}
}

// updateTailscaleMetrics periodically updates Tailscale connection metrics
func (s *Server) updateTailscaleMetrics(ctx context.Context) {
	if s.tsnetServer == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			localClient, err := s.tsnetServer.LocalClient()
			if err != nil {
				s.logger.Error("Failed to get LocalClient for metrics", "error", err)
				metrics.UpdateTailscaleStatus(false)
				continue
			}

			status, err := localClient.Status(ctx)
			if err != nil {
				s.logger.Error("Failed to get Tailscale status for metrics", "error", err)
				metrics.UpdateTailscaleStatus(false)
				continue
			}

			// Update connection count
			activeConnections := 0
			if status.Peer != nil {
				for _, peer := range status.Peer {
					if peer.Online {
						activeConnections++
					}
				}
			}

			metrics.UpdateTailscaleStatus(true)
		}
	}
}

// TailscaleDNSHandler handles DNS queries from Tailscale clients
// Provides full functionality: 4via6, MagicDNS, and backend forwarding
type TailscaleDNSHandler struct {
	config        *config.Config
	runtimeCfg    *config.RuntimeConfig
	via6Trans     *via6.Translator
	forwarder     *Forwarder
	tsnetServer   *tailscale.TSNetServer
	zoneCaches    map[string]*cache.ZoneCache
	memoryMonitor *memory.Monitor
	logger        *logger.Logger
}

// Legacy DNSHandler for backwards compatibility
type DNSHandler = TailscaleDNSHandler

// TailscaleDNSHandler.ServeDNS provides DNS functionality with feature detection based on client source
func (h *TailscaleDNSHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	clientIP := h.getClientIP(w.RemoteAddr())
	isTailscaleClient := h.isTailscaleClient(clientIP)

	// Start recording DNS query metrics
	var queryType string
	var zoneName = "default"
	if len(r.Question) > 0 {
		queryType = dns.TypeToString[r.Question[0].Qtype]
		// Try to determine zone for metrics
		if zone := h.config.GetZone(r.Question[0].Name); zone != nil {
			for name, z := range h.config.Zones {
				if z == zone {
					zoneName = name
					break
				}
			}
		}
	}

	// Record query and start timer
	done := metrics.RecordDNSQuery(zoneName, queryType)
	defer done()

	if h.runtimeCfg.LogQueries {
		for _, q := range r.Question {
			clientType := "external"
			if isTailscaleClient {
				clientType = "tailscale"
			}
			h.logger.Info("DNS query", "name", q.Name, "type", dns.TypeToString[q.Qtype], "client", clientType)
		}
	}

	for _, question := range r.Question {
		// Check cache first if zone has caching enabled
		if zoneCache, exists := h.zoneCaches[zoneName]; exists {
			clientIP := h.getClientIP(w.RemoteAddr())
			cacheKey := cache.CacheKey(question.Name, question.Qtype, clientIP.AsSlice())
			
			if cachedResponse, found := zoneCache.Get(cacheKey); found {
				metrics.RecordCacheHit(zoneName)
				metrics.UpdateCacheSize(zoneName, zoneCache.Size())
				
				// Update memory monitoring
				if h.memoryMonitor != nil {
					if err := h.memoryMonitor.UpdateCacheUsage(zoneName, zoneCache.MemoryUsage()); err != nil {
						h.logger.ZoneDebug(zoneName, "Failed to update cache usage", "error", err)
					}
				}
				
				h.logger.ZoneDebug(zoneName, "Cache hit", "domain", question.Name, "type", dns.TypeToString[question.Qtype])
				_ = w.WriteMsg(cachedResponse)
				return
			}
			metrics.RecordCacheMiss(zoneName)
		}
		
		// Priority 1: Check if it's a 4via6 zone (only for Tailscale clients)
		if isTailscaleClient {
			zone := h.config.GetZone(question.Name)
			if zone != nil && zone.Has4via6() {
				h.logger.ZoneDebug(zoneName, "4via6 translation triggered", "domain", question.Name)
				h.handleZoneQuery(w, r, question, zone, zoneName)
				return
			}
		}

		// Priority 2: Check if it's a MagicDNS domain (available for all clients)
		if h.isMagicDNSDomain(question.Name) {
			h.handleMagicDNSQuery(w, r, question)
			return
		}
	}

	// Priority 3: Forward to backend DNS servers
	// Check if there's a zone for this domain
	zone := h.config.GetZone(r.Question[0].Name)
	
	// Check access permissions
	if !isTailscaleClient && (zone == nil || !zone.AllowExternalClients) {
		// External clients can only access zones that explicitly allow them
		h.logger.Debug("External client blocked", "client", clientIP.String(), "zone", zoneName, "domain", r.Question[0].Name)
		metrics.RecordExternalClientQuery(zoneName, "blocked")
		msg := new(dns.Msg)
		msg.SetRcode(r, dns.RcodeNameError)
		_ = w.WriteMsg(msg)
		return
	}
	
	// Forward the query
	if zone != nil {
		// Log external access for security monitoring
		if !isTailscaleClient && zone.AllowExternalClients {
			h.logger.Info("External client accessing allowed zone", "client", clientIP.String(), "zone", zoneName, "domain", r.Question[0].Name)
			metrics.RecordExternalClientQuery(zoneName, "allowed")
		}
		
		// Use zone-specific backend with TSNet support (if available)
		var zoneForwarder *Forwarder
		if h.tsnetServer != nil && isTailscaleClient {
			// Tailscale clients get TSNet routing for subnet access
			zoneForwarder = NewForwarderWithTSNet(zone.Backend, h.logger, h.tsnetServer)
		} else {
			// External clients use standard DNS forwarding
			zoneForwarder = NewForwarder(zone.Backend, h.logger)
		}
		zoneCache := h.zoneCaches[zoneName]
		zoneForwarder.ForwardWithZoneAndCache(w, r, zoneName, zoneCache)
	} else {
		// Use global backend (Tailscale clients only)
		h.forwarder.ForwardWithZone(w, r, "global")
	}
}

func (h *TailscaleDNSHandler) handleZoneQuery(w dns.ResponseWriter, r *dns.Msg, question dns.Question, zone *config.Zone, zoneName string) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	if question.Qtype == dns.TypeAAAA {
		via6IP, err := h.via6Trans.TranslateToVia6(question.Name)
		if err != nil {
			h.logger.ZoneError(zoneName, "4via6 translation failed", "domain", question.Name, "error", err)
			metrics.RecordVia6Error(zoneName, "translation_failed")
		} else {
			metrics.RecordVia6Translation(zoneName)
			msg.Answer = append(msg.Answer, &dns.AAAA{
				Hdr:  dns.RR_Header{Name: question.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: h.runtimeCfg.DefaultTTL},
				AAAA: via6IP,
			})
		}
	}
	// For A queries on 4via6 domains, return NODATA (empty answer)

	// Cache the response if zone has caching enabled (before sending)
	if zoneCache, exists := h.zoneCaches[zoneName]; exists {
		cacheKey := cache.CacheKey(question.Name, question.Qtype, nil) // Remove client IP for better cache efficiency
		zoneCache.Set(cacheKey, msg)
		metrics.UpdateCacheSize(zoneName, zoneCache.Size())
		h.logger.ZoneDebug(zoneName, "Response cached", "domain", question.Name, "type", dns.TypeToString[question.Qtype])
	}
	
	_ = w.WriteMsg(msg)
}


// isMagicDNSDomain checks if domain should be resolved via MagicDNS
func (h *TailscaleDNSHandler) isMagicDNSDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	return strings.HasSuffix(domain, ".ts.net")
}

// handleMagicDNSQuery resolves MagicDNS domains using TSNet's LocalClient.Status()
func (h *TailscaleDNSHandler) handleMagicDNSQuery(w dns.ResponseWriter, r *dns.Msg, question dns.Question) {

	if h.tsnetServer == nil {
		h.logger.Warn("TSNet server not available for MagicDNS query", "domain", question.Name)
		h.forwarder.Forward(w, r)
		return
	}

	ctx := context.Background()

	localClient, err := h.tsnetServer.LocalClient()
	if err != nil {
		h.logger.Error("Failed to get LocalClient for MagicDNS", "error", err)
		h.forwarder.Forward(w, r)
		return
	}

	// Use LocalClient.Status() to resolve hostname from peer list
	domain := strings.TrimSuffix(question.Name, ".")
	ip, _, err := h.resolveHostname(ctx, localClient, domain)
	if err != nil {
		h.logger.Debug("MagicDNS resolution failed", "domain", question.Name, "error", err)

		// Return NXDOMAIN - hostname not found in tailnet
		msg := new(dns.Msg)
		msg.SetRcode(r, dns.RcodeNameError)
		_ = w.WriteMsg(msg)
		return
	}

	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	if question.Qtype == dns.TypeA && ip.Is4() {
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: h.runtimeCfg.DefaultTTL},
			A:   ip.AsSlice(),
		})
	} else if question.Qtype == dns.TypeAAAA && ip.Is6() {
		msg.Answer = append(msg.Answer, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: question.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: h.runtimeCfg.DefaultTTL},
			AAAA: ip.AsSlice(),
		})
	}

	if len(msg.Answer) == 0 {
		// No appropriate record type found
		msg.Rcode = dns.RcodeNameError
	}

	if h.runtimeCfg.LogQueries {
		h.logger.Info("MagicDNS resolved", "name", question.Name, "ip", ip.String())
	}

	// Record DNS response

	_ = w.WriteMsg(msg)
}

// resolveHostname resolves a hostname using TSNet's LocalClient.Status()
func (h *TailscaleDNSHandler) resolveHostname(ctx context.Context, localClient *local.Client, hostname string) (netip.Addr, string, error) {
	status, err := localClient.Status(ctx)
	if err != nil {
		return netip.Addr{}, "", fmt.Errorf("failed to get Tailscale status: %w", err)
	}

	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))

	// Check self
	if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
		selfDNS := strings.ToLower(strings.TrimSuffix(status.Self.DNSName, "."))
		if hostname == selfDNS || strings.HasPrefix(selfDNS, hostname+".") {
			return status.Self.TailscaleIPs[0], status.Self.DNSName, nil
		}
	}

	// Check peers
	for _, peer := range status.Peer {
		if len(peer.TailscaleIPs) == 0 {
			continue
		}
		peerDNS := strings.ToLower(strings.TrimSuffix(peer.DNSName, "."))
		if hostname == peerDNS || strings.HasPrefix(peerDNS, hostname+".") {
			return peer.TailscaleIPs[0], peer.DNSName, nil
		}
	}

	return netip.Addr{}, "", fmt.Errorf("hostname %q not found", hostname)
}

// getClientIP extracts the IP address from a remote address
func (h *TailscaleDNSHandler) getClientIP(remoteAddr net.Addr) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr.String())
	if err != nil {
		return netip.Addr{}
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}

	return ip
}

// isTailscaleClient determines if the client IP is from the Tailscale network
func (h *TailscaleDNSHandler) isTailscaleClient(clientIP netip.Addr) bool {
	if !clientIP.IsValid() {
		return false
	}

	// Allow localhost for internal testing
	if clientIP.IsLoopback() {
		return true
	}

	// Check if client IP is in Tailscale IP ranges (100.x.x.x or fd7a:115c:a1e0::/48)
	if clientIP.Is4() {
		// Tailscale IPv4 range: 100.64.0.0/10
		return clientIP.As4()[0] == 100 && (clientIP.As4()[1]&0xC0) == 0x40
	} else {
		// Tailscale IPv6 range: fd7a:115c:a1e0::/48
		ipBytes := clientIP.As16()
		return ipBytes[0] == 0xfd && ipBytes[1] == 0x7a &&
			ipBytes[2] == 0x11 && ipBytes[3] == 0x5c &&
			ipBytes[4] == 0xa1 && ipBytes[5] == 0xe0
	}
}

func NewForwarder(cfg config.BackendConfig, log *logger.Logger) *Forwarder {
	return &Forwarder{
		backends: cfg.DNSServers,
		timeout:  parseTimeout(cfg.Timeout),
		retries:  cfg.Retries,
		logger:   log,
	}
}

func NewForwarderWithTSNet(cfg config.BackendConfig, log *logger.Logger, tsnetServer *tailscale.TSNetServer) *Forwarder {
	return &Forwarder{
		backends:    cfg.DNSServers,
		timeout:     parseTimeout(cfg.Timeout),
		retries:     cfg.Retries,
		logger:      log,
		tsnetServer: tsnetServer,
	}
}

func (f *Forwarder) Forward(w dns.ResponseWriter, r *dns.Msg) {
	f.ForwardWithZone(w, r, "default")
}

// queryBackend queries a DNS backend, using TSNet if available
func (f *Forwarder) queryBackend(r *dns.Msg, backend, zoneName string) (*dns.Msg, error) {
	if f.tsnetServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
		defer cancel()
		
		conn, err := f.tsnetServer.Dial(ctx, "udp", backend)
		if err != nil {
			return nil, err
		}
		defer func() { _ = conn.Close() }()
		
		dnsConn := &dns.Conn{Conn: conn}
		client := &dns.Client{Timeout: f.timeout}
		resp, _, err := client.ExchangeWithConn(r, dnsConn)
		return resp, err
	}
	
	client := &dns.Client{Timeout: f.timeout}
	resp, _, err := client.Exchange(r, backend)
	return resp, err
}

func (f *Forwarder) ForwardWithZone(w dns.ResponseWriter, r *dns.Msg, zoneName string) {
	f.ForwardWithZoneAndCache(w, r, zoneName, nil)
}

func (f *Forwarder) ForwardWithZoneAndCache(w dns.ResponseWriter, r *dns.Msg, zoneName string, zoneCache *cache.ZoneCache) {
	var lastErr error
	for i := 0; i < f.retries; i++ {
		for _, backend := range f.backends {
			metrics.RecordBackendQuery(zoneName, backend)
			
			resp, err := f.queryBackend(r, backend, zoneName)
			if err != nil {
				lastErr = err
				metrics.RecordBackendError(zoneName, backend)
				continue
			}

			// Cache the response if cache is provided (before sending)
			if zoneCache != nil && len(r.Question) > 0 {
				cacheKey := cache.CacheKey(r.Question[0].Name, r.Question[0].Qtype, nil) // Remove client IP for better cache efficiency
				zoneCache.Set(cacheKey, resp)
				metrics.UpdateCacheSize(zoneName, zoneCache.Size())
			}

			_ = w.WriteMsg(resp)
			return
		}
	}

	f.logger.ZoneError(zoneName, "All backend DNS servers failed", "retries", f.retries, "error", lastErr)

	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Rcode = dns.RcodeServerFailure

	_ = w.WriteMsg(msg)
}

// HTTP handlers for health and metrics endpoints

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	// Simple health check - if we can respond, we're healthy
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","service":"tsdnsreflector"}`))
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	// Redirect to the main metrics endpoint
	w.Header().Set("Location", "/metrics")
	w.WriteHeader(http.StatusMovedPermanently)
	_, _ = w.Write([]byte("Metrics available at /metrics\n"))
}

// ReloadConfig applies hot-reloadable configuration changes
func (s *Server) ReloadConfig(newCfg *config.Config) error {
	if err := newCfg.ValidateZones(); err != nil {
		return fmt.Errorf("zone validation failed: %w", err)
	}

	// Logging config now comes from runtime, not from config file

	// Update 4via6 translator with new zones
	newTranslator, err := via6.NewTranslator(newCfg, s.logger)
	if err != nil {
		return fmt.Errorf("failed to create new zone-based translator: %w", err)
	}

	// Update zone caches
	newZoneCaches := make(map[string]*cache.ZoneCache)
	for zoneName, zone := range newCfg.Zones {
		if zone.Cache != nil {
			maxSize := zone.Cache.MaxSize
			if maxSize == 0 {
				maxSize = newCfg.Global.Cache.MaxSize
			}
			ttl, _ := config.ParseCacheTTL(zone.Cache.TTL)
			// Reuse existing cache if configuration unchanged
			if existingCache, exists := s.zoneCaches[zoneName]; exists {
				newZoneCaches[zoneName] = existingCache
				s.logger.ZoneDebug(zoneName, "Reusing existing zone cache")
			} else {
				newZoneCaches[zoneName] = cache.NewZoneCache(maxSize, ttl)
				s.logger.ZoneInfo(zoneName, "Zone cache created during reload", "maxSize", maxSize, "ttl", ttl)
			}
		}
	}

	// Update components
	s.config = newCfg
	s.via6Trans = newTranslator
	// Create forwarder with TSNet support if available
	if s.tsnetServer != nil {
		s.forwarder = NewForwarderWithTSNet(newCfg.Global.Backend, s.logger, s.tsnetServer)
	} else {
		s.forwarder = NewForwarder(newCfg.Global.Backend, s.logger)
	}
	s.zoneCaches = newZoneCaches

	// Update handler
	if handler, ok := s.dnsServer.Handler.(*TailscaleDNSHandler); ok {
		handler.config = newCfg
		handler.via6Trans = newTranslator
		handler.forwarder = s.forwarder
		handler.zoneCaches = s.zoneCaches
		handler.logger = s.logger
	}

	// Count zones with 4via6
	enabledZones := 0
	for _, zone := range newCfg.Zones {
		if zone.Has4via6() {
			enabledZones++
		}
	}

	s.logger.Info("Configuration reloaded", "totalZones", len(newCfg.Zones), "via6Zones", enabledZones)
	return nil
}
