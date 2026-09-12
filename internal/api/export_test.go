package api

import "github.com/the-heaven-labs/aether/internal/agent"

// RegisterToolForTest exposes the agent tool registry so external tests can
// exercise dispatch paths (e.g. MCP) with custom probe tools.
func (s *Server) RegisterToolForTest(def *agent.ToolDef) {
	s.agentEngine.GetRegistry().Register(def)
}
