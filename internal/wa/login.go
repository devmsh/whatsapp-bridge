package wa

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
)

// Login states surfaced to the GUI.
const (
	StateConnecting = "connecting"  // have a session, dialing WhatsApp
	StateLoggedOut  = "logged_out"  // no session, no QR yet
	StateQR         = "qr"          // showing a QR code, waiting for scan
	StatePairing    = "pairing"     // QR scanned, finishing pairing
	StateConnected  = "connected"   // linked and online
	StateError      = "error"       // pairing or connection error
)

// DeviceInfo describes the linked WhatsApp account.
type DeviceInfo struct {
	JID          string `json:"jid"`
	LID          string `json:"lid,omitempty"`
	PushName     string `json:"push_name,omitempty"`
	Platform     string `json:"platform,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
}

// AuthState is the snapshot the GUI renders.
type AuthState struct {
	State     string      `json:"state"`
	QRCode    string      `json:"qr_code,omitempty"` // raw data to encode as a QR image
	Error     string      `json:"error,omitempty"`
	Device    *DeviceInfo `json:"device,omitempty"`
	UpdatedAt int64       `json:"updated_at"`
}

// AuthManager tracks login state and fans changes out to subscribers (SSE).
type AuthManager struct {
	client *Client

	mu          sync.RWMutex
	state       AuthState
	subscribers map[chan AuthState]struct{}

	loginMu sync.Mutex // serializes StartLogin so we never open two QR channels
}

func newAuthManager(c *Client) *AuthManager {
	return &AuthManager{
		client:      c,
		state:       AuthState{State: StateConnecting, UpdatedAt: time.Now().Unix()},
		subscribers: make(map[chan AuthState]struct{}),
	}
}

// Snapshot returns the current auth state.
func (m *AuthManager) Snapshot() AuthState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// set mutates the state under lock and notifies subscribers.
func (m *AuthManager) set(mutate func(*AuthState)) {
	m.mu.Lock()
	mutate(&m.state)
	m.state.UpdatedAt = time.Now().Unix()
	snapshot := m.state
	for ch := range m.subscribers {
		select {
		case ch <- snapshot:
		default: // slow subscriber — drop, they'll get the next one
		}
	}
	m.mu.Unlock()
}

// Subscribe registers a listener for state changes. The current state is sent
// immediately so a new SSE client renders without waiting for the next change.
func (m *AuthManager) Subscribe() chan AuthState {
	ch := make(chan AuthState, 8)
	m.mu.Lock()
	m.subscribers[ch] = struct{}{}
	ch <- m.state
	m.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a listener channel.
func (m *AuthManager) Unsubscribe(ch chan AuthState) {
	m.mu.Lock()
	delete(m.subscribers, ch)
	m.mu.Unlock()
	close(ch)
}

// StartLogin connects an existing session or begins the QR flow when logged out.
// It returns quickly; the QR loop runs in the background and pushes state updates.
func (m *AuthManager) StartLogin(ctx context.Context) error {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()

	wa := m.client.WA

	// Existing session — just connect. Connected/LoggedOut events drive state.
	if wa.Store.ID != nil {
		m.set(func(s *AuthState) { s.State = StateConnecting; s.QRCode = ""; s.Error = "" })
		if wa.IsConnected() {
			return nil
		}
		if err := wa.Connect(); err != nil {
			m.set(func(s *AuthState) { s.State = StateError; s.Error = err.Error() })
			return fmt.Errorf("connect: %w", err)
		}
		return nil
	}

	// No session — open a QR channel before connecting.
	if wa.IsConnected() {
		wa.Disconnect()
	}
	qrChan, err := wa.GetQRChannel(ctx)
	if err != nil {
		m.set(func(s *AuthState) { s.State = StateError; s.Error = err.Error() })
		return fmt.Errorf("get qr channel: %w", err)
	}
	if err := wa.Connect(); err != nil {
		m.set(func(s *AuthState) { s.State = StateError; s.Error = err.Error() })
		return fmt.Errorf("connect: %w", err)
	}

	go m.consumeQR(qrChan)
	return nil
}

// consumeQR drains the QR channel, updating state for each event.
func (m *AuthManager) consumeQR(qrChan <-chan whatsmeow.QRChannelItem) {
	for evt := range qrChan {
		switch evt.Event {
		case whatsmeow.QRChannelEventCode:
			code := evt.Code
			m.set(func(s *AuthState) {
				s.State = StateQR
				s.QRCode = code
				s.Error = ""
				s.Device = nil
			})
			// Also render in the terminal for headless / SSH use.
			qrterminal.GenerateHalfBlock(code, qrterminal.M, os.Stdout)
			fmt.Println("Scan the QR code above, or open the GUI to scan it.")
		case "success":
			// Pairing done; PairSuccess + Connected events finish the transition.
			m.set(func(s *AuthState) { s.State = StatePairing; s.QRCode = "" })
		case "timeout":
			m.set(func(s *AuthState) {
				s.State = StateLoggedOut
				s.QRCode = ""
				s.Error = "QR code expired — start login again"
			})
		default:
			msg := evt.Event
			if evt.Error != nil {
				msg = evt.Error.Error()
			}
			m.set(func(s *AuthState) {
				s.State = StateError
				s.QRCode = ""
				s.Error = msg
			})
		}
	}
}

// Logout unlinks the device. The LoggedOut event resets state to a fresh QR.
func (m *AuthManager) Logout(ctx context.Context) error {
	if m.client.WA.Store.ID == nil {
		return nil
	}
	return m.client.WA.Logout(ctx)
}

// onPairSuccess records the linked device when pairing completes.
func (m *AuthManager) onPairSuccess(d *DeviceInfo) {
	m.set(func(s *AuthState) {
		s.State = StatePairing
		s.QRCode = ""
		s.Device = d
	})
}

// onConnected marks the client online and captures device info from the store.
func (m *AuthManager) onConnected() {
	dev := m.client.deviceInfo()
	m.set(func(s *AuthState) {
		s.State = StateConnected
		s.QRCode = ""
		s.Error = ""
		if dev != nil {
			s.Device = dev
		}
	})
}

// onLoggedOut clears state and restarts the QR flow so the GUI shows a new code.
func (m *AuthManager) onLoggedOut() {
	m.set(func(s *AuthState) {
		s.State = StateLoggedOut
		s.QRCode = ""
		s.Device = nil
	})
	// Begin a fresh QR flow shortly; the socket needs a moment to settle.
	go func() {
		time.Sleep(time.Second)
		if err := m.StartLogin(context.Background()); err != nil {
			m.client.Log.Warnf("Restart login after logout failed: %v", err)
		}
	}()
}
