package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waSyncAction "go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsapp-bridge-v2/internal/db"
)

func (s *Server) handleContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := r.URL.Query().Get("q")
	contacts, err := s.store.GetContacts(q)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if contacts == nil {
		contacts = []db.Contact{}
	}
	// NOTE: contacts are a name directory — they always return the full set,
	// regardless of hidden-chats / private-mode state. Otherwise sender names
	// and "@<digits>" mentions of hidden contacts would not resolve inside
	// non-hidden group chats (you'd see "+<digits>" instead of the real name).
	// Hidden-state filtering is enforced on chats, messages, and the SSE
	// stream — not on the contact directory.
	//
	// We DO mark each contact with is_hidden so the UI can prompt for a
	// fingerprint when the user clicks a mention chip for a hidden contact —
	// without flipping the whole UI into private mode.
	hidden := s.store.HiddenChatJIDs()
	type contactOut struct {
		db.Contact
		IsHidden bool `json:"is_hidden,omitempty"`
	}
	out := make([]contactOut, 0, len(contacts))
	for _, c := range contacts {
		isHidden := hidden[c.JID]
		// A contact may also be tracked under its LID-form JID, so check that
		// when phone-form isn't in hidden.
		if !isHidden && c.LID != "" {
			if hidden[c.LID+"@lid"] || hidden[c.LID] {
				isHidden = true
			}
		}
		out = append(out, contactOut{Contact: c, IsHidden: isHidden})
	}
	jsonOK(w, out)
}

func (s *Server) handleContactsCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Phones []string `json:"phones"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}

	wa := s.client.GetWhatsmeowClient()
	results, err := wa.IsOnWhatsApp(context.Background(), req.Phones)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("check failed: %v", err))
		return
	}

	type CheckResult struct {
		Query string `json:"query"`
		JID   string `json:"jid"`
		IsIn  bool   `json:"is_in"`
	}
	var out []CheckResult
	for _, r := range results {
		out = append(out, CheckResult{
			Query: r.Query,
			JID:   r.JID.String(),
			IsIn:  r.IsIn,
		})
	}
	jsonOK(w, out)
}

func (s *Server) handleContactByJID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/contacts/")
	if path == "check" {
		s.handleContactsCheck(w, r)
		return
	}

	parts := strings.SplitN(path, "/", 2)
	jid := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "business":
		s.handleContactBusiness(w, r, jid)
	case "avatar":
		s.handleContactAvatar(w, r, jid)
	case "name":
		s.handleContactName(w, r, jid)
	case "tags":
		s.handleContactTags(w, r, jid)
	case "dashboard":
		s.handleContactDashboard(w, r, jid)
	default:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		contact, err := s.store.GetContact(jid)
		if err != nil || contact == nil {
			jsonError(w, 404, "contact not found")
			return
		}
		jsonOK(w, contact)
	}
}

func (s *Server) handleContactBusiness(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}
	wa := s.client.GetWhatsmeowClient()
	profile, err := wa.GetBusinessProfile(context.Background(), parsedJID)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("get business profile: %v", err))
		return
	}
	jsonOK(w, profile)
}

func (s *Server) handleContactAvatar(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	params := &whatsmeow.GetProfilePictureParams{}
	if r.URL.Query().Get("preview") == "true" {
		params.Preview = true
	}

	wa := s.client.GetWhatsmeowClient()
	pic, err := wa.GetProfilePictureInfo(context.Background(), parsedJID, params)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("get avatar: %v", err))
		return
	}
	if pic == nil {
		jsonError(w, 404, "no avatar")
		return
	}
	jsonOK(w, pic)
}

func (s *Server) handleContactName(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		FullName  string `json:"full_name"`
		FirstName string `json:"first_name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if req.FullName == "" {
		jsonError(w, 400, "full_name required")
		return
	}

	parsedJID, err := types.ParseJID(jid)
	if err != nil {
		jsonError(w, 400, "invalid JID")
		return
	}

	// Sync contact name via WhatsApp App State
	wa := s.client.GetWhatsmeowClient()
	patch := appstate.PatchInfo{
		Type: appstate.WAPatchCriticalUnblockLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexContact, parsedJID.ToNonAD().String()},
			Version: 2,
			Value: &waSyncAction.SyncActionValue{
				ContactAction: &waSyncAction.ContactAction{
					FullName:  proto.String(req.FullName),
					FirstName: proto.String(req.FirstName),
				},
			},
		}},
	}

	err = wa.SendAppState(context.Background(), patch)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("set contact name: %v", err))
		return
	}

	// Update local store
	wa.Store.Contacts.PutContactName(context.Background(), parsedJID, req.FullName, req.FirstName)

	// Update our DB
	s.store.StoreContact(&db.Contact{
		JID:  jid,
		Name: req.FullName,
	})

	jsonOK(w, map[string]bool{"success": true})
}
