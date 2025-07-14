package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

type Config struct {
	Global GlobalConfig     `json:"global"`
	Zones  map[string]*Zone `json:"zones"`
}

// ServerConfig removed - moved to environment variables and flags

type GlobalConfig struct {
	Backend BackendConfig `json:"backend"`
	Cache   CacheConfig   `json:"cache"`
}

type Zone struct {
	Domains              []string      `json:"domains"`
	Backend              BackendConfig `json:"backend"`
	ReflectedDomain      string        `json:"reflectedDomain,omitempty"` // Unified reflection
	TranslateID          *uint16       `json:"translateid,omitempty"`     // Optional 4via6
	PrefixSubnet         string        `json:"prefixSubnet,omitempty"`    // Optional 4via6
	Cache                *CacheConfig  `json:"cache,omitempty"`
	AllowExternalClients bool          `json:"allowExternalClients,omitempty"` // Allow non-Tailscale clients
}

type BackendConfig struct {
	DNSServers []string `json:"dnsServers"`
	Timeout    string   `json:"timeout"`
	Retries    int      `json:"retries"`
}

type CacheConfig struct {
	MaxSize int    `json:"maxSize"`
	TTL     string `json:"ttl"`
}

// TailscaleConfig and OAuthConfig removed - moved to environment variables


// LoggingConfig removed - moved to environment variables and flags

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	ast, err := hujson.Parse(data)
	if err != nil {
		return nil, err
	}

	ast.Standardize()
	standardized := ast.Pack()

	var config Config
	if err := json.Unmarshal(standardized, &config); err != nil {
		return nil, err
	}

	if err := config.setDefaults(); err != nil {
		return nil, err
	}

	if err := config.ValidateZones(); err != nil {
		return nil, fmt.Errorf("zone validation failed: %w", err)
	}

	return &config, nil
}

func (c *Config) setDefaults() error {
	if len(c.Global.Backend.DNSServers) == 0 {
		c.Global.Backend.DNSServers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	if c.Global.Backend.Timeout == "" {
		c.Global.Backend.Timeout = "5s"
	}
	if c.Global.Backend.Retries == 0 {
		c.Global.Backend.Retries = 3
	}

	if c.Global.Cache.MaxSize == 0 {
		c.Global.Cache.MaxSize = 10000
	}
	if c.Global.Cache.TTL == "" {
		c.Global.Cache.TTL = "300s"
	}

	// Apply defaults to zones
	for zoneName, zone := range c.Zones {
		if err := c.setZoneDefaults(zoneName, zone); err != nil {
			return fmt.Errorf("failed to set defaults for zone %s: %w", zoneName, err)
		}
	}

	return nil
}

func (c *Config) setZoneDefaults(zoneName string, zone *Zone) error {
	if zoneName == "" {
		return fmt.Errorf("zone name is required")
	}

	if len(zone.Domains) == 0 {
		return fmt.Errorf("zone %s must have at least one domain", zoneName)
	}

	// Inherit global backend settings if not specified
	if len(zone.Backend.DNSServers) == 0 {
		zone.Backend.DNSServers = c.Global.Backend.DNSServers
	}
	if zone.Backend.Timeout == "" {
		zone.Backend.Timeout = c.Global.Backend.Timeout
	}
	if zone.Backend.Retries == 0 {
		zone.Backend.Retries = c.Global.Backend.Retries
	}

	// Set defaults for unified fields
	if zone.TranslateID != nil {
		if zone.PrefixSubnet == "" {
			zone.PrefixSubnet = "fd7a:115c:a1e0:b1a::/64"
		}
		if *zone.TranslateID == 0 {
			return fmt.Errorf("zone %s: translateID cannot be 0 (reserved)", zoneName)
		}
	}

	if zone.Cache == nil && c.Global.Cache.MaxSize > 0 {
		zone.Cache = &CacheConfig{
			MaxSize: c.Global.Cache.MaxSize,
			TTL:     c.Global.Cache.TTL,
		}
	}

	return nil
}
