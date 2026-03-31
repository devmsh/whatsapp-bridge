package api

import (
	"fmt"
	"net/http"

	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/wa"
)

// Server holds the HTTP server state.
type Server struct {
	store    *db.Store
	client   *wa.Client
	mediaDir string
	port     int
	mux      *http.ServeMux
}

// NewServer creates a new API server.
func NewServer(store *db.Store, client *wa.Client, mediaDir string, port int) *Server {
	s := &Server{
		store:    store,
		client:   client,
		mediaDir: mediaDir,
		port:     port,
		mux:      http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Health
	s.mux.HandleFunc("/api/v2/health", s.handleHealth)

	// Send operations
	s.mux.HandleFunc("/api/v2/send", s.handleSend)
	s.mux.HandleFunc("/api/v2/reply", s.handleReply)
	s.mux.HandleFunc("/api/v2/react", s.handleReact)
	s.mux.HandleFunc("/api/v2/mention", s.handleMention)
	s.mux.HandleFunc("/api/v2/forward", s.handleForward)

	// Messages
	s.mux.HandleFunc("/api/v2/messages", s.handleMessages)
	s.mux.HandleFunc("/api/v2/messages/mark-read", s.handleMarkRead)
	s.mux.HandleFunc("/api/v2/messages/", s.handleMessageByID)
	s.mux.HandleFunc("/api/v2/unread", s.handleUnread)

	// Chats
	s.mux.HandleFunc("/api/v2/chats", s.handleChats)
	s.mux.HandleFunc("/api/v2/chats/", s.handleChatByJID)

	// Contacts
	s.mux.HandleFunc("/api/v2/contacts", s.handleContacts)
	s.mux.HandleFunc("/api/v2/contacts/check", s.handleContactsCheck)
	s.mux.HandleFunc("/api/v2/contacts/", s.handleContactByJID)

	// Groups
	s.mux.HandleFunc("/api/v2/groups", s.handleGroups)
	s.mux.HandleFunc("/api/v2/groups/discover", s.handleGroupsDiscover)
	s.mux.HandleFunc("/api/v2/groups/join", s.handleGroupJoin)
	s.mux.HandleFunc("/api/v2/groups/", s.handleGroupByJID)

	// Presence
	s.mux.HandleFunc("/api/v2/presence", s.handlePresenceSet)
	s.mux.HandleFunc("/api/v2/presence/typing", s.handlePresenceTyping)
	s.mux.HandleFunc("/api/v2/presence/subscribe", s.handlePresenceSubscribe)
	s.mux.HandleFunc("/api/v2/presence/", s.handlePresenceGet)

	// Newsletters
	s.mux.HandleFunc("/api/v2/newsletters", s.handleNewsletters)
	s.mux.HandleFunc("/api/v2/newsletters/", s.handleNewsletterByJID)

	// Polls
	s.mux.HandleFunc("/api/v2/polls", s.handlePollCreate)
	s.mux.HandleFunc("/api/v2/polls/", s.handlePollByID)

	// Privacy
	s.mux.HandleFunc("/api/v2/privacy", s.handlePrivacy)
	s.mux.HandleFunc("/api/v2/blocklist", s.handleBlocklist)

	// Status
	s.mux.HandleFunc("/api/v2/status/about", s.handleStatusAbout)
	s.mux.HandleFunc("/api/v2/status/privacy", s.handleStatusPrivacy)

	// Calls
	s.mux.HandleFunc("/api/v2/calls", s.handleCallsList)
	s.mux.HandleFunc("/api/v2/calls/", s.handleCallByID)

	// Media
	s.mux.HandleFunc("/api/v2/media/", s.handleMedia)

	// Scan (CSV endpoints for HQ intelligence)
	s.mux.HandleFunc("/api/v2/scan/messages", s.handleScanMessages)
	s.mux.HandleFunc("/api/v2/scan/groups", s.handleScanGroups)

	// Sync
	s.mux.HandleFunc("/api/v2/sync/contacts", s.handleSyncContacts)
	s.mux.HandleFunc("/api/v2/sync/history", s.handleSyncHistory)
	s.mux.HandleFunc("/api/v2/sync/state/", s.handleSyncState)
}

// Start begins listening on the configured port.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("API server starting on %s\n", addr)
	return http.ListenAndServe(addr, s.mux)
}
