//go:build lite
package mcp

import (
	"github.com/mark3labs/mcp-go/server"
)

func (s *State) registerAdvancedTools(srv *server.MCPServer) {
	// Advanced tools are not available in Lite version
}
