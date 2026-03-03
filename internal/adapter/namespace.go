package adapter

// Namespace constants for Go→MCP tool name projection.
// Each namespace corresponds to a canonical Go interface.
const (
	NSMemory   = "memory"   // MemoryProvider interface
	NSSession  = "session"  // Storage interface
	NSContext  = "context"  // ContextManager interface
	NSState    = "state"    // StateManager interface
	NSProvider = "provider" // Provider interface
	NSEmbed    = "embed"    // Embedder interface
)
