package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	wamcp "whatsapp-bridge-v2/internal/mcp"
)

func main() {
	dbPath := os.Getenv("WA_DB_PATH")
	if dbPath == "" {
		dbPath = "/Users/devmsh/whatsapp-bridge/store/messages.db"
	}

	apiURL := os.Getenv("WA_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8082/api/v2"
	}

	srv, err := wamcp.New(dbPath, apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start MCP server: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := srv.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
