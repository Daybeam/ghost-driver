//go:build !lite && !saas
package mcp

import (
	"github.com/mark3labs/mcp-go/server"
)

func (s *State) registerAuthTools(srv *server.MCPServer) {
	// Auth tools are only for Lite and SaaS versions
}
