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
	BindAddr          string
	DBPath            string
	WADBPath          string
	MediaDir          string
	LogLevel          string
	ElevenLabsAPIKey  string
	ElevenLabsVoiceID string

	// Media auto-download defaults. GUI changes persist in the DB and override these.
	MediaImages    bool
	MediaVideo     bool
	MediaAudio     bool
	MediaDocuments bool
	MediaStickers  bool
	MediaMaxSizeMB int

	// History sync period — applied only at pairing (re-link to change).
	HistoryPeriod string // "3months" | "1year" | "everything"
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
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
		BindAddr: "127.0.0.1", // localhost only; set BRIDGE_BIND=0.0.0.0 to expose
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
	if v := os.Getenv("BRIDGE_BIND"); v != "" {
		c.BindAddr = v
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

	// Media auto-download defaults — download everything, no size cap.
	c.MediaImages = envBool("BRIDGE_MEDIA_IMAGES", true)
	c.MediaVideo = envBool("BRIDGE_MEDIA_VIDEO", true)
	c.MediaAudio = envBool("BRIDGE_MEDIA_AUDIO", true)
	c.MediaDocuments = envBool("BRIDGE_MEDIA_DOCUMENTS", true)
	c.MediaStickers = envBool("BRIDGE_MEDIA_STICKERS", true)
	c.MediaMaxSizeMB = envInt("BRIDGE_MEDIA_MAX_SIZE_MB", 0)

	// History period default — WhatsApp's normal recent window.
	c.HistoryPeriod = "3months"
	if v := os.Getenv("BRIDGE_HISTORY_PERIOD"); v != "" {
		c.HistoryPeriod = v
	}

	return c
}
