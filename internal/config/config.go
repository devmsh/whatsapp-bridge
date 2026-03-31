package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the bridge.
type Config struct {
	Port     int
	DBPath   string
	WADBPath string
	MediaDir string
	LogLevel string
}

// Load reads configuration from environment variables with defaults.
func Load() *Config {
	c := &Config{
		Port:     8082,
		DBPath:   "store/messages.db",
		WADBPath: "store/whatsapp.db",
		MediaDir: "store",
		LogLevel: "info",
	}
	if v := os.Getenv("BRIDGE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := os.Getenv("BRIDGE_DB_PATH"); v != "" {
		c.DBPath = v
	}
	if v := os.Getenv("BRIDGE_WA_DB_PATH"); v != "" {
		c.WADBPath = v
	}
	if v := os.Getenv("BRIDGE_MEDIA_DIR"); v != "" {
		c.MediaDir = v
	}
	if v := os.Getenv("BRIDGE_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	return c
}
