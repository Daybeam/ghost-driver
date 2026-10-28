package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Browser Tuning
	DefaultMouseJitter   float64
	DefaultMouseSteps    int
	MouseStepDelay       time.Duration
	MouseClickHoldTime   time.Duration
	DefaultTypingDelay   time.Duration

	// Visual Loop Tuning
	DefaultAutoRecordFPS int
	DefaultRecordFrames  int

	// Viewport Defaults
	DefaultViewportWidth  float64
	DefaultViewportHeight float64

	// Wait Conditions
	DefaultMaxWait         time.Duration
	NetworkIdleTimeout     time.Duration
	DOMQuietTimeout        time.Duration
	VisualHashTimeout      time.Duration

	// Stealth
	StealthEnabled bool

	// Advanced
	CDPHostMap string

	// SaaS & Auth
	APIKey string
	UserID string

	// External Tools
	FFmpegPath string

	// VLM
	VLMProvider string
	VLMAPIKey   string
	VLMModel    string
	VLMBaseURL  string

	// Storage
	TopologyDBPath string

	// Toolset selection (comma-separated, empty = all)
	Toolsets string
}

func LoadConfig() *Config {
	return &Config{
		DefaultMouseJitter:   getEnvFloat("GHOST_MOUSE_JITTER", 4.0),
		DefaultMouseSteps:    getEnvInt("GHOST_MOUSE_STEPS", 20),
		MouseStepDelay:       getEnvDuration("GHOST_MOUSE_STEP_DELAY", 8*time.Millisecond),
		MouseClickHoldTime:   getEnvDuration("GHOST_CLICK_HOLD", 40*time.Millisecond),
		DefaultTypingDelay:   getEnvDuration("GHOST_TYPING_DELAY", 50*time.Millisecond),
		DefaultAutoRecordFPS: getEnvInt("GHOST_RECORD_FPS", 8),
		DefaultRecordFrames:  getEnvInt("GHOST_RECORD_FRAMES", 10),
		DefaultViewportWidth:  getEnvFloat("GHOST_VIEWPORT_WIDTH", 1280.0),
		DefaultViewportHeight: getEnvFloat("GHOST_VIEWPORT_HEIGHT", 720.0),
		DefaultMaxWait:        getEnvDuration("GHOST_MAX_WAIT", 15*time.Second),
		NetworkIdleTimeout:    getEnvDuration("GHOST_NET_IDLE", 500*time.Millisecond),
		DOMQuietTimeout:       getEnvDuration("GHOST_DOM_QUIET", 300*time.Millisecond),
		VisualHashTimeout:     getEnvDuration("GHOST_VIS_HASH", 400*time.Millisecond),
		StealthEnabled:        getEnvBool("GHOST_STEALTH", false),
		CDPHostMap:            os.Getenv("GHOST_CDP_HOST_MAP"),
		APIKey:                os.Getenv("GHOST_API_KEY"),
		UserID:                os.Getenv("GHOST_USER_ID"),
		FFmpegPath:            os.Getenv("GHOST_FFMPEG_PATH"),
		VLMProvider:           getEnvStr("GHOST_VLM_PROVIDER", "gemini"),
		VLMAPIKey:             getEnvStr2("GHOST_VLM_API_KEY", "GEMINI_API_KEY"),
		VLMModel:              os.Getenv("GHOST_VLM_MODEL"),
		VLMBaseURL:            os.Getenv("GHOST_VLM_BASE_URL"),
		TopologyDBPath:        os.Getenv("GHOST_TOPOLOGY_DB"),
		Toolsets:              os.Getenv("GHOST_TOOLSETS"),
	}
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvStr2(primary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	return os.Getenv(fallback)
}
