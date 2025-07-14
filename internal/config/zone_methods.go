package config

import (
	"fmt"
	"strings"
	"time"
)

func (c *Config) GetZone(domain string) *Zone {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}

	var bestMatch *Zone
	var bestMatchLength int

	for _, zone := range c.Zones {
		// Zone is enabled simply by existing in the configuration
		for _, zoneDomain := range zone.Domains {
			if zone.MatchesDomain(domain, zoneDomain) {
				// Prefer more specific matches (longer domain patterns)
				domainLength := len(zoneDomain)
				if bestMatch == nil || domainLength > bestMatchLength {
					bestMatch = zone
					bestMatchLength = domainLength
				}
			}
		}
	}

	return bestMatch
}

// MatchesDomain checks if a domain matches a zone domain pattern
func (z *Zone) MatchesDomain(domain, zoneDomain string) bool {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	if !strings.HasSuffix(zoneDomain, ".") {
		zoneDomain += "."
	}

	// Handle wildcard patterns
	if strings.HasPrefix(zoneDomain, "*.") {
		suffix := zoneDomain[1:] // Remove the *
		return strings.HasSuffix(domain, suffix)
	}

	// Exact match or subdomain match
	return domain == zoneDomain || strings.HasSuffix(domain, "."+zoneDomain)
}

func (c *Config) ValidateZones() error {
	if len(c.Zones) == 0 {
		return fmt.Errorf("at least one zone must be configured")
	}

	zoneNames := make(map[string]bool)
	translateIDs := make(map[uint16]string)

	for name, zone := range c.Zones {
		// Check zone name uniqueness
		if zoneNames[name] {
			return fmt.Errorf("duplicate zone name: %s", name)
		}
		zoneNames[name] = true

		if len(zone.Domains) == 0 {
			return fmt.Errorf("zone %s must have at least one domain", name)
		}

		if len(zone.Backend.DNSServers) == 0 {
			return fmt.Errorf("zone %s must have at least one backend DNS server", name)
		}

		if zone.Backend.Timeout != "" {
			if _, err := time.ParseDuration(zone.Backend.Timeout); err != nil {
				return fmt.Errorf("zone %s has invalid timeout: %w", name, err)
			}
		}

		// Validate unified reflection configuration
		if zone.HasReflection() {
			reflectedDomain := zone.GetReflectedDomain()
			if reflectedDomain == "" {
				return fmt.Errorf("zone %s: reflectedDomain is required when reflection is configured", name)
			}
		}

		// Validate 4via6 configuration (unified approach)
		if zone.Has4via6() {
			translateID := zone.GetTranslateID()
			if translateID == 0 {
				return fmt.Errorf("zone %s: translateID cannot be 0 (reserved)", name)
			}

			// Check for duplicate translateIDs
			if existingZone, exists := translateIDs[translateID]; exists {
				return fmt.Errorf("duplicate translateID %d in zones %s and %s", translateID, existingZone, name)
			}
			translateIDs[translateID] = name

			// Ensure 4via6 zones have reflection configured
			if !zone.HasReflection() {
				return fmt.Errorf("zone %s: reflectedDomain is required when 4via6 (translateID) is configured", name)
			}
		}


		if zone.Cache != nil && zone.Cache.TTL != "" {
			if _, err := time.ParseDuration(zone.Cache.TTL); err != nil {
				return fmt.Errorf("zone %s has invalid cache TTL: %w", name, err)
			}
		}

		// Warn about external client access
		if zone.AllowExternalClients {
			// This is just validation, actual warning logging happens at runtime
			// when we have access to the logger
			if zone.Has4via6() {
				return fmt.Errorf("zone %s: cannot allow external clients on 4via6 zones for security reasons", name)
			}
		}
	}

	return nil
}

// HasReflection returns true if this zone has reflection configured
func (z *Zone) HasReflection() bool {
	return z.ReflectedDomain != ""
}

// Has4via6 returns true if this zone has 4via6 translation configured
func (z *Zone) Has4via6() bool {
	return z.TranslateID != nil && *z.TranslateID != 0
}


// GetReflectedDomain returns the reflected domain for this zone
func (z *Zone) GetReflectedDomain() string {
	return z.ReflectedDomain
}

// GetTranslateID returns the translate ID for 4via6
func (z *Zone) GetTranslateID() uint16 {
	if z.TranslateID != nil {
		return *z.TranslateID
	}
	return 0
}

// GetPrefixSubnet returns the prefix subnet for 4via6
func (z *Zone) GetPrefixSubnet() string {
	if z.PrefixSubnet != "" {
		return z.PrefixSubnet
	}
	return "fd7a:115c:a1e0:b1a::/64" // Default
}

// GetCacheTTL returns the cache TTL as a time.Duration
func (c *CacheConfig) GetTTL() time.Duration {
	if c.TTL == "" {
		return 300 * time.Second
	}

	duration, err := time.ParseDuration(c.TTL)
	if err != nil {
		return 300 * time.Second
	}

	return duration
}
