package api

import (
	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/agent"
)

// WarehouseSyncerForTest mirrors the warehouse sync trigger surface so
// external tests can record enqueues without a real sync worker.
type WarehouseSyncerForTest interface {
	Enqueue(uuid.UUID)
}

// SetWarehouseSyncerForTest replaces the server's warehouse sync service. The
// replaced service is closed when it owns a Close method (the production
// *chaccess.SyncService does), so worker state cannot leak past the swap;
// recorders do not implement Close and are left alone.
func (s *Server) SetWarehouseSyncerForTest(syncer WarehouseSyncerForTest) {
	if s.warehouseSync != syncer {
		if closer, ok := s.warehouseSync.(interface{ Close() }); ok {
			closer.Close()
		}
	}
	s.warehouseSync = syncer
}

// RegisterToolForTest exposes the agent tool registry so external tests can
// exercise dispatch paths (e.g. MCP) with custom probe tools.
func (s *Server) RegisterToolForTest(def *agent.ToolDef) {
	s.agentEngine.GetRegistry().Register(def)
}

// MCPToolAllowlistForTest exposes the curated MCP catalog so external tests can
// assert tools/list matches it exactly.
func (s *Server) MCPToolAllowlistForTest() []string {
	mcpAllowlistMu.RLock()
	defer mcpAllowlistMu.RUnlock()
	names := make([]string, 0, len(mcpToolAllowlist))
	for name := range mcpToolAllowlist {
		names = append(names, name)
	}
	return names
}

// SetMCPToolAllowedForTest temporarily adds or removes a tool from the MCP
// allowlist so dispatch-path tests can use probe tools.
func (s *Server) SetMCPToolAllowedForTest(name string, allowed bool) {
	mcpAllowlistMu.Lock()
	defer mcpAllowlistMu.Unlock()
	if allowed {
		mcpToolAllowlist[name] = struct{}{}
	} else {
		delete(mcpToolAllowlist, name)
	}
}
