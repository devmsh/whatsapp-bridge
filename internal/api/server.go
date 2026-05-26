package api

import (
	"fmt"
	"io/fs"
	"net/http"

	"whatsapp-bridge-v2/internal/config"
	"whatsapp-bridge-v2/internal/db"
	"whatsapp-bridge-v2/internal/wa"
)

// Server holds the HTTP server state.
type Server struct {
	store      *db.Store
	client     *wa.Client
	mediaDir   string
	port       int
	mux        *http.ServeMux
	cfg        *config.Config
	webFS      fs.FS
	fileServer http.Handler
	profiles           *ProfileManager
	runs               *RunManager
	autoExtract        *AutoExtractor
	mediaUnderstanding *MediaUnderstandingManager
	hiddenUnlocker     *HiddenUnlocker
}

// StartProfiler starts the background entity-profiling worker and daily refresh.
// Also starts the continuous-extraction scheduler and media-understanding
// workers (all idle unless enabled).
func (s *Server) StartProfiler() {
	s.profiles.Start()
	s.autoExtract.Start()
	s.mediaUnderstanding.Start()
}

// NewServer creates a new API server. webFS is the embedded web UI (may be nil).
func NewServer(store *db.Store, client *wa.Client, mediaDir string, port int, cfg *config.Config, webFS fs.FS) *Server {
	s := &Server{
		store:    store,
		client:   client,
		mediaDir: mediaDir,
		port:     port,
		mux:      http.NewServeMux(),
		cfg:      cfg,
		webFS:    webFS,
	}
	if webFS != nil {
		s.fileServer = http.FileServerFS(webFS)
	}
	s.profiles = newProfileManager(s)
	s.runs = newRunManager()
	s.autoExtract = newAutoExtractor(s)
	s.mediaUnderstanding = newMediaManager(s)
	s.hiddenUnlocker = newHiddenUnlocker()
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Health
	s.mux.HandleFunc("/api/v2/health", s.handleHealth)

	// Auth / onboarding
	s.mux.HandleFunc("/api/v2/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("/api/v2/auth/stream", s.handleAuthStream)
	s.mux.HandleFunc("/api/v2/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("/api/v2/auth/logout", s.handleAuthLogout)

	// Send operations
	s.mux.HandleFunc("/api/v2/send", s.handleSend)
	s.mux.HandleFunc("/api/v2/reply", s.handleReply)
	s.mux.HandleFunc("/api/v2/react", s.handleReact)
	s.mux.HandleFunc("/api/v2/tts-send", s.handleTTSSend)
	s.mux.HandleFunc("/api/v2/mention", s.handleMention)
	s.mux.HandleFunc("/api/v2/forward", s.handleForward)
	s.mux.HandleFunc("/api/v2/uploads", s.handleUpload)

	// Messages
	s.mux.HandleFunc("/api/v2/messages", s.handleMessages)
	s.mux.HandleFunc("/api/v2/messages/mark-read", s.handleMarkRead)
	s.mux.HandleFunc("/api/v2/messages/", s.handleMessageByID)
	s.mux.HandleFunc("/api/v2/unread", s.handleUnread)
	s.mux.HandleFunc("/api/v2/starred", s.handleStarredList)

	// Chats
	s.mux.HandleFunc("/api/v2/chats", s.handleChats)
	s.mux.HandleFunc("/api/v2/chats/", s.handleChatByJID)

	// Contacts
	s.mux.HandleFunc("/api/v2/contacts", s.handleContacts)
	s.mux.HandleFunc("/api/v2/contacts/check", s.handleContactsCheck)
	s.mux.HandleFunc("/api/v2/contacts/tags", s.handleContactTagsMap)
	s.mux.HandleFunc("/api/v2/contacts/", s.handleContactByJID)

	// Tags
	s.mux.HandleFunc("/api/v2/tags", s.handleTags)
	s.mux.HandleFunc("/api/v2/tags/", s.handleTagByID)

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
	// Single-shot typing snapshot for the chat list — every chat with a
	// fresh 'composing' beacon, returned in one call. See handler doc.
	s.mux.HandleFunc("/api/v2/typing", s.handleTypingSnapshot)

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
	s.mux.HandleFunc("/api/v2/avatars/", s.handleAvatar)

	// Scan (CSV endpoints for HQ intelligence)
	s.mux.HandleFunc("/api/v2/scan/messages", s.handleScanMessages)
	s.mux.HandleFunc("/api/v2/scan/groups", s.handleScanGroups)

	// Stream (SSE real-time message push)
	s.mux.HandleFunc("/api/v2/stream", s.handleStream)

	// Circles (clusters of groups/contacts/circles)
	s.mux.HandleFunc("/api/v2/circles", s.handleCircles)
	s.mux.HandleFunc("/api/v2/circles/recommendations", s.handleCircleRecommendations)
	s.mux.HandleFunc("/api/v2/circles/recommendations/dismiss", s.handleRecDismiss)
	s.mux.HandleFunc("/api/v2/circles/recommendations/restore", s.handleRecRestore)
	s.mux.HandleFunc("/api/v2/circles/for-member", s.handleCircleForMember)
	s.mux.HandleFunc("/api/v2/circles/", s.handleCircleByID)

	// Tasks (work items on top of WhatsApp content)
	s.mux.HandleFunc("/api/v2/tasks", s.handleTasks)
	s.mux.HandleFunc("/api/v2/tasks/extract", s.handleTaskExtract)
	s.mux.HandleFunc("/api/v2/tasks/cluster", s.handleClusterTasks)
	s.mux.HandleFunc("/api/v2/tasks/", s.handleTaskByID)

	// Extraction history (read straight from the Agent SDK session store, no DB)
	s.mux.HandleFunc("/api/v2/extractions", s.handleExtractions)
	s.mux.HandleFunc("/api/v2/extractions/transcript", s.handleExtractionTranscript)
	s.mux.HandleFunc("/api/v2/extractions/mark", s.handleExtractionMark)
	s.mux.HandleFunc("/api/v2/extractions/runs", s.handleRunsRoot)
	s.mux.HandleFunc("/api/v2/extractions/runs/", s.handleRunsRoot)

	// Daily briefings (AI digest of tasks + signal chats + awaiting-reply)
	s.mux.HandleFunc("/api/v2/briefings", s.handleBriefingsRoot)
	s.mux.HandleFunc("/api/v2/briefings/", s.handleBriefingsRoot)

	// Auto / continuous extraction
	s.mux.HandleFunc("/api/v2/extractions/auto", s.handleAutoExtract)

	// Universal search (contacts + groups + circles + tasks + messages)
	s.mux.HandleFunc("/api/v2/search", s.handleSearch)

	// Media understanding (voice transcription + image description)
	s.mux.HandleFunc("/api/v2/media/understanding", s.handleMediaUnderstanding)

	// Hidden chats — lock / unlock with PIN + Touch ID.
	s.mux.HandleFunc("/api/v2/hidden/status", s.handleHiddenStatus)
	s.mux.HandleFunc("/api/v2/hidden/list", s.handleHiddenList)
	s.mux.HandleFunc("/api/v2/hidden/pin/setup", s.handleHiddenPinSetup)
	s.mux.HandleFunc("/api/v2/hidden/unlock/pin", s.handleHiddenUnlockPin)
	s.mux.HandleFunc("/api/v2/hidden/webauthn/register/options", s.handleHiddenWARegisterOptions)
	s.mux.HandleFunc("/api/v2/hidden/webauthn/register/verify", s.handleHiddenWARegisterVerify)
	s.mux.HandleFunc("/api/v2/hidden/webauthn/auth/options", s.handleHiddenWAAuthOptions)
	s.mux.HandleFunc("/api/v2/hidden/webauthn/auth/verify", s.handleHiddenWAAuthVerify)
	s.mux.HandleFunc("/api/v2/hidden/webauthn/chat/options", s.handleHiddenWAChatOptions)
	s.mux.HandleFunc("/api/v2/hidden/webauthn/chat/verify", s.handleHiddenWAChatVerify)
	s.mux.HandleFunc("/api/v2/hidden/lock", s.handleHiddenLock)

	// Entity profiles (AI-written purpose descriptions; background-refreshed)
	s.mux.HandleFunc("/api/v2/profiles", s.handleProfile)
	s.mux.HandleFunc("/api/v2/profiles/regenerate", s.handleProfileRegenerate)
	s.mux.HandleFunc("/api/v2/profiles/status", s.handleProfilesStatus)

	// Stats
	s.mux.HandleFunc("/api/v2/stats/messages", s.handleStatsMessages)

	// Settings
	s.mux.HandleFunc("/api/v2/settings/media", s.handleSettingsMedia)
	s.mux.HandleFunc("/api/v2/settings/history", s.handleSettingsHistory)

	// Sync
	s.mux.HandleFunc("/api/v2/sync/progress", s.handleSyncProgress)
	s.mux.HandleFunc("/api/v2/sync/contacts", s.handleSyncContacts)
	s.mux.HandleFunc("/api/v2/sync/history", s.handleSyncHistory)
	s.mux.HandleFunc("/api/v2/sync/app-state-replay", s.handleSyncAppStateReplay)
	s.mux.HandleFunc("/api/v2/sync/migrate-lid", s.handleSyncMigrateLID)
	s.mux.HandleFunc("/api/v2/sync/state/", s.handleSyncState)

	// Embedded web UI (catch-all — least specific, matched last).
	if s.fileServer != nil {
		s.mux.HandleFunc("/", s.handleSPA)
	}
}

// Start begins listening on the configured bind address and port.
func (s *Server) Start() error {
	bind := s.cfg.BindAddr
	if bind == "" {
		bind = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bind, s.port)
	fmt.Printf("Server listening on http://%s\n", addr)
	return http.ListenAndServe(addr, s.mux)
}
