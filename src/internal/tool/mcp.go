package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

// MCPManager manages all MCP connections.
type MCPManager struct {
	connections map[string]*MCPConnection
	mu          sync.RWMutex
	tools       *Tools // Reference to parent Tools instance
}

// MCPConnection represents a single connection to an MCP server.
type MCPConnection struct {
	Config    MCPServerConfig
	Session   *mcp.ClientSession
	IsEnabled bool
}

// init initializes MCP connections from database
func (m *MCPManager) init(tools *Tools, db *sql.DB) {
	m.connections = make(map[string]*MCPConnection)
	m.tools = tools

	configs, err := database.GetMCPServerConfigs(db)
	if err != nil {
		log.Printf("Error loading MCP server configs: %v", err)
		return
	}

	for _, config := range configs {
		if config.Enabled {
			m.StartConnection(config)
		}
	}
	log.Println("MCP Manager initialized.")
}

// StartConnection starts a connection to an MCP server
func (m *MCPManager) StartConnection(config MCPServerConfig) {
	log.Printf("Attempting to connect to MCP server: %s", config.Name)

	var connDetails struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal(config.ConfigJSON, &connDetails); err != nil {
		log.Printf("Error parsing MCP config for %s: %v", config.Name, err)
		return
	}

	ctx := context.Background()
	transport := mcp.NewSSEClientTransport(connDetails.Endpoint, nil)

	// The first argument to NewClient cannot be nil.
	client := mcp.NewClient(&mcp.Implementation{}, nil)

	session, err := client.Connect(ctx, transport)
	if err != nil {
		log.Printf("Failed to connect to MCP server %s: %v", config.Name, err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	conn := &MCPConnection{
		Config:    config,
		Session:   session,
		IsEnabled: true,
	}
	m.connections[config.Name] = conn

	log.Printf("MCP connection '%s' to %s established.", config.Name, connDetails.Endpoint)
}

// StopConnection stops a connection to an MCP server
func (m *MCPManager) StopConnection(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, ok := m.connections[name]
	if !ok {
		return
	}

	if conn.Session != nil {
		conn.Session.Close()
	}
	log.Printf("Stopping MCP connection: %s", conn.Config.Name)
	delete(m.connections, name)
}

// GetMCPConnections returns a snapshot of the current MCP connections.
func (m *MCPManager) GetMCPConnections() map[string]*MCPConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	connsCopy := make(map[string]*MCPConnection)
	for name, conn := range m.connections {
		connsCopy[name] = conn
	}
	return connsCopy
}

// DispatchToolCall sends a tool call to the appropriate MCP server.
func (m *MCPManager) DispatchToolCall(ctx context.Context, mcpServerName string, toolName string, args map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	conn, ok := m.connections[mcpServerName]
	m.mu.RUnlock()

	if !ok || !conn.IsEnabled {
		return nil, fmt.Errorf("mcp server not found or not enabled: %s", mcpServerName)
	}

	params := &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	}

	result, err := conn.Session.CallTool(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("mcp tool call failed: %w", err)
	}

	// Extract the text content from the result.
	// The result can have multiple content parts, but for now we'll just concatenate them.
	var responseText string
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			responseText += textContent.Text
		}
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(responseText), &resultMap); err != nil {
		// If the response is not a valid JSON, return it as a raw string.
		return map[string]interface{}{"result": responseText}, nil
	}

	return resultMap, nil
}
