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

	if strings.HasPrefix(zoneDomain, "*.") {
		suffix := zoneDomain[1:]
		return strings.HasSuffix(domain, suffix)
	}
	return domain == zoneDomain || strings.HasSuffix(domain, "."+zoneDomain)
}

func (c *Config) ValidateZones() error {
	if len(c.Zones) == 0 {
		return fmt.Errorf("no zones configured")
	}

	translateIDs := make(map[uint16]string)

	for name, zone := range c.Zones {
		if len(zone.Domains) == 0 {
			return fmt.Errorf("zone %s: no domains", name)
		}

		if len(zone.Backend.DNSServers) == 0 {
			return fmt.Errorf("zone %s: no DNS servers", name)
		}

		if zone.Backend.Timeout != "" {
			if _, err := time.ParseDuration(zone.Backend.Timeout); err != nil {
				return fmt.Errorf("zone %s: bad timeout", name)
			}
		}

		if zone.Has4via6() {
			id := *zone.TranslateID
			if id == 0 {
				return fmt.Errorf("zone %s: translateID cannot be 0", name)
			}
			if existing, dup := translateIDs[id]; dup {
				return fmt.Errorf("translateID %d used by %s and %s", id, existing, name)
			}
			translateIDs[id] = name

			if zone.ReflectedDomain == "" {
				return fmt.Errorf("zone %s: needs reflectedDomain for 4via6", name)
			}
		}

		if zone.Cache != nil && zone.Cache.TTL != "" {
			if _, err := time.ParseDuration(zone.Cache.TTL); err != nil {
				return fmt.Errorf("zone %s: bad cache TTL", name)
			}
		}

		if zone.AllowExternalClients && zone.Has4via6() {
			return fmt.Errorf("zone %s: no external clients on 4via6", name)
		}
	}

	return nil
}

func (z *Zone) HasReflection() bool {
	return z.ReflectedDomain != ""
}

func (z *Zone) Has4via6() bool {
	return z.TranslateID != nil && *z.TranslateID != 0
}


