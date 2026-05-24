package api

import (
	"net/http"
	"strings"
	"time"
)

// handleChatHidePreview returns what will be deleted when this chat is hidden,
// so the UI can show a confirmation dialog before the irreversible cleanup.
// GET /api/v2/chats/{jid}/hide-preview
func (s *Server) handleChatHidePreview(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.isUnlocked(r) {
		jsonError(w, 401, "unlock required to manage hidden chats")
		return
	}
	jsonOK(w, s.store.HidePreviewFor(jid))
}

// handleChatHide marks the chat hidden AND clears every piece of AI-derived
// data about it. Returns a report of what was removed. Also wipes extraction
// session files (out-of-band, via the sidecar).
// POST /api/v2/chats/{jid}/hide
func (s *Server) handleChatHide(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.isUnlocked(r) {
		jsonError(w, 401, "unlock required to manage hidden chats")
		return
	}
	res, err := s.store.HideChatAndClear(jid)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Best-effort wipe of stored Claude session files for chat-level
	// extractions of this JID. Don't fail the request if this doesn't run.
	go func() {
		_, _ = s.runAgent(60*time.Second, "sessions.mjs", "delete-for-chat", jid)
	}()
	jsonOK(w, res)
}

func (s *Server) handleChatUnhide(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.isUnlocked(r) {
		jsonError(w, 401, "unlock required to manage hidden chats")
		return
	}
	if err := s.store.UnhideChat(jid); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"jid": jid, "hidden": false})
}

// handleHiddenList returns the hidden chat JIDs. Requires unlock so the list
// isn't enumerable without authenticating.
func (s *Server) handleHiddenList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.isUnlocked(r) {
		jsonError(w, 401, "unlock required")
		return
	}
	jsonOK(w, map[string]any{"jids": s.store.HiddenChatJIDsList()})
}

// stripHiddenJIDs filters a slice of JIDs by removing the hidden ones, unless
// the request is unlocked. Used to apply UI filtering consistently.
func (s *Server) stripHiddenJIDs(r *http.Request, jids []string) []string {
	if s.isUnlocked(r) {
		return jids
	}
	hidden := s.store.HiddenChatJIDs()
	if len(hidden) == 0 {
		return jids
	}
	out := jids[:0]
	for _, j := range jids {
		if !hidden[j] {
			out = append(out, j)
		}
	}
	return out
}

// notFoundIfHidden returns true (and writes 404) when the chat is hidden and
// the request isn't unlocked. Used to make per-chat endpoints behave as if the
// hidden chat doesn't exist.
func (s *Server) notFoundIfHidden(w http.ResponseWriter, r *http.Request, jid string) bool {
	if !s.store.IsChatHidden(jid) {
		return false
	}
	if s.isUnlocked(r) {
		return false
	}
	jsonError(w, 404, "not found")
	return true
}

// notAllowedForAI returns true (and writes 403) when the chat is hidden — used
// by AI endpoints (draft replies, extractions, dashboards) which must NEVER
// process hidden chats, even when the session is unlocked.
func (s *Server) notAllowedForAI(w http.ResponseWriter, jid string) bool {
	if !s.store.IsChatHidden(jid) {
		return false
	}
	jsonError(w, 403, "AI features are disabled for hidden chats")
	return true
}

// hiddenBlobFilter returns a SQL fragment + args that excludes hidden chats
// from a query. Caller appends `<fragment>` to a WHERE clause and the args.
// Pattern: " AND chat_jid NOT IN (?,?,?)".
func (s *Server) hiddenBlobFilter(column string) (string, []any) {
	hidden := s.store.HiddenChatJIDsList()
	if len(hidden) == 0 {
		return "", nil
	}
	args := make([]any, len(hidden))
	for i, j := range hidden {
		args[i] = j
	}
	return " AND " + column + " NOT IN (" + strings.Repeat("?,", len(hidden)-1) + "?)", args
}
