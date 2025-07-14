package config

import (
	"time"
)

func ParseTimeout(timeoutStr string) (time.Duration, error) {
	if timeoutStr == "" {
		return 5 * time.Second, nil
	}
	return time.ParseDuration(timeoutStr)
}

func ParseCacheTTL(ttlStr string) (time.Duration, error) {
	if ttlStr == "" {
		return 5 * time.Minute, nil
	}
	return time.ParseDuration(ttlStr)
}
