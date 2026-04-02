package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the bridge.
type Config struct {
	Port              int
	DBPath            string
	WADBPath          string
	MediaDir          string
	LogLevel          string
	ElevenLabsAPIKey  string
	ElevenLabsVoiceID string
}

// loadEnvFile reads a .env file and sets env vars (does not override existing).
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

// Load reads configuration from .env file then environment variables.
func Load() *Config {
	loadEnvFile(".env")
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
	if v := os.Getenv("ELEVENLABS_API_KEY"); v != "" {
		c.ElevenLabsAPIKey = v
	}
	if v := os.Getenv("ELEVENLABS_VOICE_ID"); v != "" {
		c.ElevenLabsVoiceID = v
	}
	return c
}
