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

	// One-time backfill: pull participants of groups already in circles in as
	// contacts (group members added before auto-import existed). Future group
	// adds are handled automatically by the API layer.
	if v, _, _ := store.GetSyncState("circle_contact_backfill_v1"); v == "" {
		ownPhone := ""
		if client.WA.Store.ID != nil {
			ownPhone = client.WA.Store.ID.User
		}
		if n := store.BackfillGroupContacts(ownPhone); n > 0 {
			fmt.Printf("Backfilled %d circle contacts from group members\n", n)
		}
		store.PutSyncState("circle_contact_backfill_v1", "done")
	}

	// Media auto-download policy: env defaults, overridden by any GUI choice
	// persisted in the DB.
	client.InitMediaPolicy(wa.MediaPolicy{
		Images:    cfg.MediaImages,
		Video:     cfg.MediaVideo,
		Audio:     cfg.MediaAudio,
		Documents: cfg.MediaDocuments,
		Stickers:  cfg.MediaStickers,
		MaxSizeMB: cfg.MediaMaxSizeMB,
	})

	// History sync period: applied to whatsmeow's device props before pairing.
	wa.ApplyHistoryPeriod(client.HistoryPeriodOr(cfg.HistoryPeriod))

	// Start API server first so the GUI is reachable during QR login.
	server := api.NewServer(store, client, cfg.MediaDir, cfg.Port, cfg, webUI())
	go func() {
		if err := server.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "API server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Connect to WhatsApp (begins QR login if no session). Non-blocking:
	// login state is surfaced through the auth API for the GUI to render.
	go func() {
		if err := client.Connect(); err != nil {
			client.Log.Errorf("Connect failed: %v", err)
		}
	}()

	// Start periodic sync (every 6 hours)
	wa.StartPeriodicSync(client, 6*time.Hour)

	fmt.Printf("Bridge running. Open the GUI at http://localhost:%d/\n", cfg.Port)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	client.Disconnect()
}
