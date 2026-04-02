package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"whatsapp-bridge-v2/internal/api"
	"whatsapp-bridge-v2/internal/config"
	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/wa"
)

func main() {
	cfg := config.Load()

	fmt.Printf("WhatsApp Bridge V2 starting (port=%d, db=%s)\n", cfg.Port, cfg.DBPath)

	// Open message database
	store, err := db.NewStore(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Create WhatsApp client
	client, err := wa.NewClient(cfg.WADBPath, store, cfg.MediaDir, cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create WA client: %v\n", err)
		os.Exit(1)
	}

	// Register event handlers
	wa.RegisterHandlers(client)

	// Connect to WhatsApp
	if err := client.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}

	// Start periodic sync (every 6 hours)
	wa.StartPeriodicSync(client, 6*time.Hour)

	// Start API server (blocks in goroutine)
	server := api.NewServer(store, client, cfg.MediaDir, cfg.Port, cfg)
	go func() {
		if err := server.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "API server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Printf("Bridge running. API at http://localhost:%d/api/v2/health\n", cfg.Port)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	client.Disconnect()
}
