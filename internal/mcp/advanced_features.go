//go:build !lite
package mcp

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/vision"
)

func (s *State) initAdvancedFeatures() {
	// FFmpeg path from config
	if s.cfg.FFmpegPath != "" {
		browser.FFmpegPath = s.cfg.FFmpegPath
	}

	// Topology DB: "off" disables topology entirely (A/B testing);
	// explicit config > binary-relative default
	dbPath := s.cfg.TopologyDBPath
	if dbPath == "off" {
		fmt.Fprintf(os.Stderr, "[ghost-driver] topology disabled (GHOST_TOPOLOGY_DB=off)\n")
	} else {
		if dbPath == "" {
			dbPath = defaultTopologyDBPath()
		}
		os.MkdirAll(filepath.Dir(dbPath), 0755)

		userID := s.cfg.UserID
		if userID == "" {
			userID = "local_user"
		}

		topo, err := browser.NewTopology(dbPath, userID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to initialize topology database at %s: %v\n", dbPath, err)
		} else {
			s.topology = topo
		}
	}

	// VLM: provider + API key + model + baseURL from config
	s.visionFinder = vision.NewFinder(s.cfg.VLMProvider, s.cfg.VLMAPIKey, s.cfg.VLMModel, s.cfg.VLMBaseURL)
}

// defaultTopologyDBPath returns a path relative to the binary location.
func defaultTopologyDBPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "./memory/topology.db"
	}
	return filepath.Join(filepath.Dir(exe), "memory", "topology.db")
}
