package wa

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatsapp-bridge-v2/internal/db"
)

// Client wraps whatsmeow.Client with our database store.
type Client struct {
	WA          *whatsmeow.Client
	Store       *db.Store
	MediaDir    string
	Log         waLog.Logger
	Broadcaster *Broadcaster
	startTime   time.Time
	mu          sync.RWMutex
}

// NewClient creates a new WhatsApp client backed by the given DB paths.
func NewClient(waDBPath string, store *db.Store, mediaDir string, logLevel string) (*Client, error) {
	logger := waLog.Stdout("Bridge", logLevel, true)

	container, err := sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", waDBPath),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("open wa store: %w", err)
	}

	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}

	waClient := whatsmeow.NewClient(device, logger)

	return &Client{
		WA:          waClient,
		Store:       store,
		MediaDir:    mediaDir,
		Log:         logger,
		Broadcaster: NewBroadcaster(),
	}, nil
}

// Connect establishes the WhatsApp connection. Shows QR if needed.
func (c *Client) Connect() error {
	if c.WA.Store.ID == nil {
		// No session, need QR scan
		qrChan, _ := c.WA.GetQRChannel(context.Background())
		if err := c.WA.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.M, os.Stdout)
				fmt.Println("Scan the QR code above to log in")
			} else {
				fmt.Println("QR event:", evt.Event)
			}
		}
	} else {
		if err := c.WA.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
	}
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()
	return nil
}

// Disconnect cleanly shuts down the WhatsApp connection.
func (c *Client) Disconnect() {
	c.WA.Disconnect()
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	return c.WA.IsConnected()
}

// GetWhatsmeowClient returns the underlying whatsmeow client.
func (c *Client) GetWhatsmeowClient() *whatsmeow.Client {
	return c.WA
}

// ResolveLIDForJID takes a phone JID (e.g. "972592604155@s.whatsapp.net") and returns
// the corresponding LID JID if one exists, or empty string if not found.
// This is used by the API layer to merge LID-stored messages with phone-stored messages.
func (c *Client) ResolveLIDForJID(phoneJID string) string {
	jid, err := types.ParseJID(phoneJID)
	if err != nil || jid.Server != types.DefaultUserServer {
		return ""
	}
	lid, err := c.WA.Store.LIDs.GetLIDForPN(context.Background(), jid)
	if err != nil || lid.IsEmpty() {
		return ""
	}
	return lid.String()
}

// ResolvePhoneForLID takes a LID JID and returns the phone JID, or empty string.
func (c *Client) ResolvePhoneForLID(lidJID string) string {
	jid, err := types.ParseJID(lidJID)
	if err != nil || jid.Server != "lid" {
		return ""
	}
	pn, err := c.WA.Store.LIDs.GetPNForLID(context.Background(), jid)
	if err != nil || pn.IsEmpty() {
		return ""
	}
	return pn.String()
}

// Uptime returns how long the client has been running.
func (c *Client) Uptime() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.startTime.IsZero() {
		return 0
	}
	return time.Since(c.startTime)
}
