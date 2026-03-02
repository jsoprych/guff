package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// MCPClient communicates with an MCP server over stdio (JSON-RPC 2.0).
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID atomic.Int64
}

// MCPServerConfig describes how to launch an MCP server.
type MCPServerConfig struct {
	Name    string            `mapstructure:"name" json:"name"`
	Command string            `mapstructure:"command" json:"command"`
	Args    []string          `mapstructure:"args" json:"args,omitempty"`
	Env     map[string]string `mapstructure:"env" json:"env,omitempty"`
}

// JSON-RPC 2.0 types for MCP protocol

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP protocol response types

type mcpToolsList struct {
	Tools []mcpToolDef `json:"tools"`
}

type mcpToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// NewMCPClient launches an MCP server process and returns a client.
func NewMCPClient(ctx context.Context, cfg MCPServerConfig) (*MCPClient, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)

	// Set environment variables
	if len(cfg.Env) > 0 {
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp start %q: %w", cfg.Command, err)
	}

	client := &MCPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
	}

	// Initialize the MCP session
	if err := client.initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}

	return client, nil
}

// initialize sends the MCP initialize handshake.
func (c *MCPClient) initialize(ctx context.Context) error {
	_, err := c.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "guff",
			"version": "0.1.0",
		},
	})
	if err != nil {
		return err
	}

	// Send initialized notification
	return c.notify("notifications/initialized", nil)
}

// ListTools asks the MCP server for its available tools.
func (c *MCPClient) ListTools(ctx context.Context) ([]ToolDef, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var list mcpToolsList
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("parse tools list: %w", err)
	}

	defs := make([]ToolDef, len(list.Tools))
	for i, t := range list.Tools {
		defs[i] = ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		}
	}
	return defs, nil
}

// CallTool invokes a tool on the MCP server.
func (c *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	result, err := c.call(ctx, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": json.RawMessage(args),
	})
	if err != nil {
		return "", err
	}

	var callResult mcpCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return "", fmt.Errorf("parse call result: %w", err)
	}

	if callResult.IsError {
		var text string
		for _, c := range callResult.Content {
			if c.Type == "text" {
				text += c.Text
			}
		}
		return "", fmt.Errorf("%w: %s", ErrToolExecFailed, text)
	}

	var text string
	for _, c := range callResult.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return text, nil
}

// Close shuts down the MCP server process.
func (c *MCPClient) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

// call sends a JSON-RPC request and waits for the response.
func (c *MCPClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Write request + newline
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response lines until we get one with our ID
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip non-JSON lines
		}

		if resp.ID == id {
			if resp.Error != nil {
				return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

	if err := c.stdout.Err(); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return nil, fmt.Errorf("mcp server closed connection")
}

// notify sends a JSON-RPC notification (no response expected).
func (c *MCPClient) notify(method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Notifications have no ID
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

// RegisterMCPTools connects to an MCP server and registers all its tools in the registry.
func RegisterMCPTools(ctx context.Context, registry *Registry, cfg MCPServerConfig) (*MCPClient, error) {
	client, err := NewMCPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("list tools from %s: %w", cfg.Name, err)
	}

	for _, tool := range tools {
		toolName := tool.Name
		registry.Register(tool, func(ctx context.Context, args json.RawMessage) (string, error) {
			return client.CallTool(ctx, toolName, args)
		})
	}

	return client, nil
}
