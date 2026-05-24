package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mark3labs/mcp-go/server"
)

// Server is the WhatsApp MCP server. It reads directly from SQLite
// and writes via the REST API of the running bridge daemon.
type Server struct {
	db     *sql.DB
	apiURL string
}

// New creates a new MCP server with a read-only SQLite connection
// and a base URL for the bridge REST API.
func New(dbPath, apiURL string) (*Server, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000", dbPath)
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Verify connection
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Server{db: sqlDB, apiURL: apiURL}, nil
}

// Serve starts the MCP server on stdio (blocks until context is cancelled).
func (s *Server) Serve(ctx context.Context) error {
	defer s.db.Close()

	mcpServer := server.NewMCPServer(
		"whatsapp-bridge",
		"2.0.0",
		server.WithToolCapabilities(true),
	)

	s.registerTools(mcpServer)

	stdio := server.NewStdioServer(mcpServer)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

func (s *Server) registerTools(srv *server.MCPServer) {
	// Read tools
	srv.AddTool(toolHealth(), s.handleHealth)
	srv.AddTool(toolReadMessages(), s.handleReadMessages)
	srv.AddTool(toolListChats(), s.handleListChats)
	srv.AddTool(toolFindContact(), s.handleFindContact)
	srv.AddTool(toolFindGroup(), s.handleFindGroup)
	srv.AddTool(toolSearchMessages(), s.handleSearchMessages)

	// Composite tools
	srv.AddTool(toolGroupInfo(), s.handleGroupInfo)
	srv.AddTool(toolScan(), s.handleScan)

	// Task tools (read DB + write via REST API)
	srv.AddTool(toolCreateTask(), s.handleCreateTask)
	srv.AddTool(toolLinkTaskMessage(), s.handleLinkTaskMessage)
	srv.AddTool(toolListTasks(), s.handleListTasks)

	// Circle + profile tools (read DB) for circle-level extraction
	srv.AddTool(toolCircleInfo(), s.handleCircleInfo)
	srv.AddTool(toolListCircles(), s.handleListCircles)
	srv.AddTool(toolGetProfile(), s.handleGetProfile)

	// Write tools (via REST API)
	srv.AddTool(toolSend(), s.handleSend)
	srv.AddTool(toolReply(), s.handleReply)
	srv.AddTool(toolReact(), s.handleReact)
	srv.AddTool(toolMention(), s.handleMention)
	srv.AddTool(toolDelete(), s.handleDelete)
	srv.AddTool(toolEdit(), s.handleEdit)
	srv.AddTool(toolMarkRead(), s.handleMarkRead)
	srv.AddTool(toolTTSSend(), s.handleTTSSend)
}
