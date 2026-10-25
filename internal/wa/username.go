package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

// UsernameLookupResult is a live usync username query result.
// JID is the addressing JID the server returned (often a LID when
// addressing_mode=lid). LID and PhoneJID are the two identity forms when
// the server supplied them.
type UsernameLookupResult struct {
	Username string `json:"username"`
	JID      string `json:"jid,omitempty"`
	LID      string `json:"lid,omitempty"`
	PhoneJID string `json:"phone_jid,omitempty"`
	Found    bool   `json:"found"`
	// RawNode is the usync response XML. The REST layer copies it into
	// "debug" when ?debug=1; it is never JSON-encoded from this struct.
	RawNode string `json:"-"`
}

// LookupUsername asks WhatsApp for the account behind @username and returns
// its LID / phone JID. It does not use the local contact table as the source
// of truth. A found result is stored onto contacts.username and, when both
// identity forms are present, into whatsmeow's LID↔PN map.
func (c *Client) LookupUsername(ctx context.Context, username string) (UsernameLookupResult, error) {
	username = normalizeLiveUsername(username)
	if username == "" {
		return UsernameLookupResult{}, fmt.Errorf("username required")
	}
	if err := c.requireWA(); err != nil {
		return UsernameLookupResult{}, err
	}

	internals := c.WA.DangerousInternals()
	usync := usernameLookupUsyncNode(internals.GenerateRequestID(), username)
	resp, err := internals.SendIQ(ctx, whatsmeow.DangerousInfoQuery{
		Namespace: "usync",
		Type:      whatsmeow.DangerousInfoQueryType("get"),
		To:        types.ServerJID,
		Content:   []waBinary.Node{usync},
	})
	if err != nil {
		return UsernameLookupResult{}, fmt.Errorf("username usync: %w", err)
	}

	result := firstUsernameResult(resp, username)
	if resp != nil {
		result.RawNode = resp.String()
	}
	c.storeUsernameResult(ctx, result)
	return result, nil
}

// FetchUsernames asks WhatsApp for the @username of known JIDs using the same
// usync username protocol. Found usernames are stored like LookupUsername.
func (c *Client) FetchUsernames(ctx context.Context, jids []string) ([]UsernameLookupResult, error) {
	if err := c.requireWA(); err != nil {
		return nil, err
	}
	parsed := make([]types.JID, 0, len(jids))
	for _, raw := range jids {
		jid, err := types.ParseJID(strings.TrimSpace(raw))
		if err != nil || jid.IsEmpty() {
			continue
		}
		parsed = append(parsed, jid)
	}
	if len(parsed) == 0 {
		return []UsernameLookupResult{}, nil
	}

	list, err := c.WA.DangerousInternals().Usync(ctx, parsed, "query", "interactive", usernameQueryNodes())
	if err != nil {
		return nil, fmt.Errorf("username usync: %w", err)
	}
	results := parseUsernameUsyncUsers(listNode(list))
	raw := ""
	if list != nil {
		raw = list.String()
	}
	out := make([]UsernameLookupResult, 0, len(results))
	for _, r := range results {
		r.RawNode = raw
		c.storeUsernameResult(ctx, r)
		out = append(out, r)
	}
	return out, nil
}

func (c *Client) requireWA() error {
	if c == nil || c.WA == nil {
		return fmt.Errorf("whatsapp client not ready")
	}
	if !c.WA.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return nil
}

func (c *Client) storeUsernameResult(ctx context.Context, r UsernameLookupResult) {
	if !r.Found || c == nil || c.Store == nil {
		return
	}
	lid := attrParseJID(r.LID)
	pn := attrParseJID(r.PhoneJID)
	primary := attrParseJID(r.JID)
	if pn.IsEmpty() && primary.Server == types.DefaultUserServer {
		pn = primary
	}
	if lid.IsEmpty() && primary.Server == types.HiddenUserServer {
		lid = primary
	}
	if c.WA != nil && !lid.IsEmpty() && !pn.IsEmpty() {
		c.WA.StoreLIDPNMapping(ctx, lid, pn)
	}

	contactJID := r.PhoneJID
	if contactJID == "" {
		contactJID = r.JID
	}
	if contactJID == "" {
		contactJID = r.LID
	}
	if contactJID == "" {
		return
	}
	_ = c.Store.StoreContact(&db.Contact{
		JID:      contactJID,
		LID:      lid.User,
		Phone:    pn.User,
		Username: r.Username,
	})
}

func normalizeLiveUsername(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "@")
	return strings.ToLower(strings.TrimSpace(s))
}

// usernameQueryNodes is the <query> payload: contact (LID addressing),
// username, and lid protocols.
func usernameQueryNodes() []waBinary.Node {
	return []waBinary.Node{
		{Tag: "contact", Attrs: waBinary.Attrs{"addressing_mode": "lid"}},
		{Tag: "username"},
		{Tag: "lid"},
	}
}

// usernameLookupUserNode is the <user> entry for a username-keyed usync.
//
// The child <username>NAME</username> is the lookup key this bridge sends.
// Official clients (Android / Baileys onWhatsAppUsername) identify the target
// with <contact username="NAME"/> instead; that sibling is included so the
// contact protocol has a user element. See the summary in README.
func usernameLookupUserNode(username string) waBinary.Node {
	return waBinary.Node{
		Tag: "user",
		Content: []waBinary.Node{
			{Tag: "username", Content: username},
			{Tag: "contact", Attrs: waBinary.Attrs{"username": username}},
		},
	}
}

func usernameLookupUsyncNode(sid, username string) waBinary.Node {
	return waBinary.Node{
		Tag: "usync",
		Attrs: waBinary.Attrs{
			"sid":     sid,
			"mode":    "query",
			"last":    "true",
			"index":   "0",
			"context": "interactive",
		},
		Content: []waBinary.Node{
			{Tag: "query", Content: usernameQueryNodes()},
			{Tag: "list", Content: []waBinary.Node{usernameLookupUserNode(username)}},
		},
	}
}

func firstUsernameResult(resp *waBinary.Node, queried string) UsernameLookupResult {
	if resp == nil {
		return UsernameLookupResult{Username: queried, Found: false}
	}
	list := listNode(resp)
	results := parseUsernameUsyncUsers(list)
	if len(results) == 0 {
		return UsernameLookupResult{Username: queried, Found: false}
	}
	r := results[0]
	if r.Username == "" {
		r.Username = queried
	}
	return r
}

func listNode(n *waBinary.Node) waBinary.Node {
	if n == nil {
		return waBinary.Node{}
	}
	if n.Tag == "list" {
		return *n
	}
	if list, ok := n.GetOptionalChildByTag("usync", "list"); ok {
		return list
	}
	if list, ok := n.GetOptionalChildByTag("list"); ok {
		return list
	}
	return *n
}

func parseUsernameUsyncUsers(list waBinary.Node) []UsernameLookupResult {
	children := list.GetChildren()
	out := make([]UsernameLookupResult, 0, len(children))
	for _, child := range children {
		if child.Tag != "user" {
			continue
		}
		out = append(out, parseUsernameUserNode(child))
	}
	return out
}

func parseUsernameUserNode(user waBinary.Node) UsernameLookupResult {
	jid := attrAsJID(user, "jid")
	pn := attrAsJID(user, "pn_jid")

	lidNode := user.GetChildByTag("lid")
	lid := attrAsJID(lidNode, "val")
	if lid.IsEmpty() {
		lid = attrAsJID(lidNode, "jid")
	}
	if lid.IsEmpty() {
		lid = attrAsJID(user, "lid")
	}

	usernameNode := user.GetChildByTag("username")
	username := nodeText(usernameNode)
	if username == "" {
		if v, ok := usernameNode.Attrs["value"].(string); ok {
			username = v
		}
	}
	contact := user.GetChildByTag("contact")
	if username == "" {
		if v, ok := contact.Attrs["username"].(string); ok {
			username = v
		}
	}
	if username == "" {
		if v, ok := user.Attrs["username"].(string); ok {
			username = v
		}
	}

	contactType, _ := contact.Attrs["type"].(string)
	if jid.IsEmpty() && !pn.IsEmpty() {
		jid = pn
	}
	if lid.IsEmpty() && jid.Server == types.HiddenUserServer {
		lid = jid
	}
	if pn.IsEmpty() && jid.Server == types.DefaultUserServer {
		pn = jid
	}

	found := contactType != "out" && (!jid.IsEmpty() || !lid.IsEmpty() || !pn.IsEmpty())
	return UsernameLookupResult{
		Username: username,
		JID:      jidString(jid),
		LID:      jidString(lid),
		PhoneJID: jidString(pn),
		Found:    found,
	}
}

func attrAsJID(n waBinary.Node, key string) types.JID {
	if n.Attrs == nil {
		return types.EmptyJID
	}
	switch v := n.Attrs[key].(type) {
	case types.JID:
		return v
	case string:
		return attrParseJID(v)
	}
	return types.EmptyJID
}

func attrParseJID(s string) types.JID {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.EmptyJID
	}
	jid, err := types.ParseJID(s)
	if err != nil {
		return types.EmptyJID
	}
	return jid
}

func jidString(jid types.JID) string {
	if jid.IsEmpty() {
		return ""
	}
	return jid.String()
}

func nodeText(n waBinary.Node) string {
	switch v := n.Content.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}
