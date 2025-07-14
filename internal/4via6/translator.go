package via6

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

const (
	Via6PrefixBase = "fd7a:115c:a1e0:b1a:0000:0000:0000:0000"
)

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

type Translator struct {
	zones  map[string]*ZoneTranslator
	config *config.Config
	logger *logger.Logger
}

type ZoneTranslator struct {
	zoneName      string
	zone          *config.Zone
	rule          *Rule
	prefixNetwork *net.IPNet
}

type Rule struct {
	ReflectedDomain string
	PrefixSubnet    string
	TranslateID     uint16
	PrefixNetwork   *net.IPNet
	DNSServers      []string
	DNSTimeout      time.Duration
}

func NewTranslator(cfg *config.Config, log *logger.Logger) (*Translator, error) {
	zones := make(map[string]*ZoneTranslator)

	log.Debug("Creating zone-based 4via6 translator", "zoneCount", len(cfg.Zones))

	for name, zone := range cfg.Zones {
		// Zone is enabled by being present in configuration

		// Only create zone translator if 4via6 is configured (unified approach)
		if !zone.Has4via6() {
			log.Debug("Skipping zone without 4via6", "zone", name)
			continue
		}

		log.ZoneInfo(name, "Adding 4via6 zone",
			"domains", zone.Domains,
			"reflectedDomain", zone.ReflectedDomain,
			"translateID", zone.TranslateID)

		zoneTranslator, err := newZoneTranslator(name, zone)
		if err != nil {
			return nil, fmt.Errorf("invalid 4via6 zone %s: %w", name, err)
		}

		zones[name] = zoneTranslator
	}

	log.Info("Zone-based 4via6 translator created successfully", "activeZones", len(zones))

	return &Translator{
		zones:  zones,
		config: cfg,
		logger: log,
	}, nil
}

func newZoneTranslator(zoneName string, zone *config.Zone) (*ZoneTranslator, error) {
	if zone.TranslateID == nil || *zone.TranslateID == 0 {
		return nil, fmt.Errorf("translateID cannot be 0 (reserved)")
	}
	translateID := *zone.TranslateID

	prefixSubnet := zone.PrefixSubnet
	if prefixSubnet == "" {
		prefixSubnet = "fd7a:115c:a1e0:b1a::/64"
	}
	_, prefixNet, err := net.ParseCIDR(prefixSubnet)
	if err != nil {
		return nil, fmt.Errorf("invalid prefix subnet %s: %w", prefixSubnet, err)
	}

	if !is4via6Prefix(prefixNet) {
		return nil, fmt.Errorf("prefix subnet %s is not within 4via6 space (must start with fd7a:115c:a1e0:b1a:)", prefixSubnet)
	}

	rule := &Rule{
		ReflectedDomain: zone.ReflectedDomain,
		PrefixSubnet:    prefixSubnet,
		TranslateID:     translateID,
		PrefixNetwork:   prefixNet,
		DNSServers:      zone.Backend.DNSServers,
		DNSTimeout:      parseTimeout(zone.Backend.Timeout),
	}

	return &ZoneTranslator{
		zoneName:      zoneName,
		zone:          zone,
		rule:          rule,
		prefixNetwork: prefixNet,
	}, nil
}

func (t *Translator) ShouldTranslate(domain string) bool {
	zone := t.config.GetZone(domain)
	return zone != nil && zone.Has4via6()
}

func (t *Translator) TranslateToVia6(domain string) (net.IP, error) {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}

	zoneTranslator := t.GetZoneForDomain(domain)
	if zoneTranslator == nil {
		return nil, fmt.Errorf("no 4via6 zone found for domain %s", domain)
	}

	return zoneTranslator.CreateVia6Address(domain, t)
}

func (t *Translator) TranslateFromVia6(via6IP net.IP) (string, net.IP, error) {
	if len(via6IP) != 16 {
		return "", nil, fmt.Errorf("invalid IPv6 address length")
	}

	if !t.isVia6Address(via6IP) {
		return "", nil, fmt.Errorf("not a 4via6 address")
	}

	translateID := (uint16(via6IP[10]) << 8) | uint16(via6IP[11])
	ipv4 := net.IP(via6IP[12:16])

	for _, zoneTranslator := range t.zones {
		if zoneTranslator.rule.TranslateID == translateID {
			return zoneTranslator.rule.ReflectedDomain, ipv4, nil
		}
	}
	return "", nil, fmt.Errorf("no zone found for translate ID %d", translateID)
}

// is4via6Prefix validates that a network prefix is within the 4via6 address space
func is4via6Prefix(network *net.IPNet) bool {
	// Check if prefix starts with fd7a:115c:a1e0:b1a:
	expectedPrefix := []byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a}
	ip := network.IP.To16()
	if ip == nil || len(ip) != 16 {
		return false
	}

	for i := 0; i < len(expectedPrefix); i++ {
		if ip[i] != expectedPrefix[i] {
			return false
		}
	}
	return true
}

func (t *Translator) GetZoneForDomain(domain string) *ZoneTranslator {
	zone := t.config.GetZone(domain)
	if zone == nil || !zone.Has4via6() {
		return nil
	}

	for _, zt := range t.zones {
		if zt.zone == zone {
			return zt
		}
	}

	return nil
}


func (t *Translator) isVia6Address(ip net.IP) bool {
	if len(ip) != 16 {
		return false
	}

	// Check if it starts with fd7a:115c:a1e0:b1a:0000
	prefix := []byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a, 0x00, 0x00}

	for i := 0; i < len(prefix); i++ {
		if ip[i] != prefix[i] {
			return false
		}
	}

	return true
}

func (zt *ZoneTranslator) CreateVia6Address(domain string, translator *Translator) (net.IP, error) {
	var ipv4 net.IP
	var err error

	if zt.rule.ReflectedDomain != "" {
		translator.logger.ZoneDebug(zt.zoneName, "Resolving reflected domain",
			"originalDomain", domain,
			"reflectedDomain", zt.rule.ReflectedDomain,
			"translateID", zt.rule.TranslateID)

		ipv4, err = zt.resolveReflectedDomain(domain, translator)
		if err != nil {
			translator.logger.Warn("Failed to resolve reflected domain",
				"zone", zt.zoneName,
				"domain", domain,
				"reflectedDomain", zt.rule.ReflectedDomain,
				"error", err)
			return nil, fmt.Errorf("failed to resolve reflected domain: %w", err)
		}

		translator.logger.Debug("Resolved reflected domain successfully",
			"zone", zt.zoneName,
			"domain", domain,
			"reflectedDomain", zt.rule.ReflectedDomain,
			"resolvedIP", ipv4.String())
	} else {
		return nil, fmt.Errorf("no reflected domain configured for zone %s", zt.zoneName)
	}

	via6 := make(net.IP, 16)
	copy(via6, zt.rule.PrefixNetwork.IP)

	via6[10] = byte(zt.rule.TranslateID >> 8)
	via6[11] = byte(zt.rule.TranslateID)

	copy(via6[12:], ipv4.To4())

	translator.logger.Debug("Created 4via6 address",
		"zone", zt.zoneName,
		"originalDomain", domain,
		"ipv4", ipv4.String(),
		"via6", via6.String(),
		"translateID", zt.rule.TranslateID)

	return via6, nil
}

func (zt *ZoneTranslator) resolveReflectedDomain(originalDomain string, translator *Translator) (net.IP, error) {
	reflectedDomain := zt.rule.ReflectedDomain

	if ip := net.ParseIP(reflectedDomain); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4, nil
		}
		return nil, fmt.Errorf("IPv6 addresses not supported")
	}

	// Handle subdomain mapping
	for _, zoneDomain := range zt.zone.Domains {
		if zt.zone.MatchesDomain(originalDomain, zoneDomain) {
			// Extract subdomain and replace the zone suffix with reflected domain
			if strings.HasPrefix(zoneDomain, "*.") {
				// Remove the wildcard prefix to get the base domain
				baseDomain := strings.TrimPrefix(zoneDomain, "*.")
				if !strings.HasSuffix(baseDomain, ".") {
					baseDomain += "."
				}
				if !strings.HasSuffix(originalDomain, ".") {
					originalDomain += "."
				}
				// Replace the zone's base domain with the reflected domain
				if strings.HasSuffix(originalDomain, baseDomain) {
					prefix := strings.TrimSuffix(originalDomain, baseDomain)
					if !strings.HasSuffix(reflectedDomain, ".") {
						reflectedDomain += "."
					}
					reflectedDomain = prefix + reflectedDomain
				}
			}
			break
		}
	}

	if !strings.HasSuffix(reflectedDomain, ".") {
		reflectedDomain += "."
	}

	client := &dns.Client{Timeout: zt.rule.DNSTimeout}
	msg := new(dns.Msg)
	msg.SetQuestion(reflectedDomain, dns.TypeA)

	for _, backend := range zt.rule.DNSServers {
		resp, _, err := client.Exchange(msg, backend)
		if err != nil {
			continue
		}
		if resp.Rcode != dns.RcodeSuccess {
			continue
		}
		for _, rr := range resp.Answer {
			if a, ok := rr.(*dns.A); ok {
				return a.A, nil
			}
		}
	}
	return nil, fmt.Errorf("no IPv4 address found for %s", reflectedDomain)
}
