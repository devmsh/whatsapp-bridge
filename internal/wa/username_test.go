package wa

import (
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func TestUsernameLookupUsyncNode(t *testing.T) {
	n := usernameLookupUsyncNode("sid-test", "alice")
	if n.Tag != "usync" {
		t.Fatalf("tag = %q", n.Tag)
	}
	for key, want := range map[string]string{
		"sid": "sid-test", "mode": "query", "last": "true", "index": "0", "context": "interactive",
	} {
		got, _ := n.Attrs[key].(string)
		if got != want {
			t.Errorf("attr %s = %q, want %q", key, got, want)
		}
	}

	query := n.GetChildByTag("query")
	children := query.GetChildren()
	if len(children) != 3 {
		t.Fatalf("query children = %d, want 3: %+v", len(children), children)
	}
	if children[0].Tag != "contact" {
		t.Errorf("query[0] = %q, want contact", children[0].Tag)
	}
	if got, _ := children[0].Attrs["addressing_mode"].(string); got != "lid" {
		t.Errorf("contact addressing_mode = %q, want lid", got)
	}
	if children[1].Tag != "username" {
		t.Errorf("query[1] = %q, want username", children[1].Tag)
	}
	if children[2].Tag != "lid" {
		t.Errorf("query[2] = %q, want lid", children[2].Tag)
	}

	list := n.GetChildByTag("list")
	user := list.GetChildByTag("user")
	if _, ok := user.GetOptionalChildByTag("username"); ok {
		t.Errorf("user must not carry a <username> child; the key is contact@username")
	}
	contact := user.GetChildByTag("contact")
	if got, _ := contact.Attrs["username"].(string); got != "alice" {
		t.Errorf("user contact username = %q, want alice", got)
	}
}

func TestNormalizeLiveUsername(t *testing.T) {
	cases := map[string]string{
		"@Alice":  "alice",
		" Alice ": "alice",
		"@@bob":   "@bob",
		"":        "",
		"@":       "",
	}
	for in, want := range cases {
		if got := normalizeLiveUsername(in); got != want {
			t.Errorf("normalizeLiveUsername(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseUsernameUserNodeFound(t *testing.T) {
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	pn := types.NewJID("966500000101", types.DefaultUserServer)
	user := waBinary.Node{
		Tag: "user",
		Attrs: waBinary.Attrs{
			"jid":    lid,
			"pn_jid": pn,
		},
		Content: []waBinary.Node{
			{Tag: "contact", Attrs: waBinary.Attrs{"type": "in"}},
			{Tag: "username", Content: "alice"},
			{Tag: "lid", Attrs: waBinary.Attrs{"val": lid}},
		},
	}
	got := parseUsernameUserNode(user)
	if !got.Found {
		t.Fatal("expected found")
	}
	if got.Username != "alice" {
		t.Errorf("username = %q", got.Username)
	}
	if got.JID != lid.String() {
		t.Errorf("jid = %q, want %s", got.JID, lid)
	}
	if got.LID != lid.String() {
		t.Errorf("lid = %q, want %s", got.LID, lid)
	}
	if got.PhoneJID != pn.String() {
		t.Errorf("phone_jid = %q, want %s", got.PhoneJID, pn)
	}
}

func TestParseUsernameUserNodeUsernameBytes(t *testing.T) {
	lid := types.NewJID("1", types.HiddenUserServer)
	user := waBinary.Node{
		Tag:   "user",
		Attrs: waBinary.Attrs{"jid": lid},
		Content: []waBinary.Node{
			{Tag: "username", Content: []byte("Bob")},
		},
	}
	got := parseUsernameUserNode(user)
	if !got.Found || got.Username != "Bob" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseUsernameUserNodeContactAttr(t *testing.T) {
	pn := types.NewJID("966500000102", types.DefaultUserServer)
	user := waBinary.Node{
		Tag:   "user",
		Attrs: waBinary.Attrs{"jid": pn},
		Content: []waBinary.Node{
			{Tag: "contact", Attrs: waBinary.Attrs{"type": "in", "username": "carol"}},
		},
	}
	got := parseUsernameUserNode(user)
	if !got.Found || got.Username != "carol" || got.PhoneJID != pn.String() {
		t.Fatalf("got %+v", got)
	}
}

func TestParseUsernameUserNodeNotFoundOut(t *testing.T) {
	user := waBinary.Node{
		Tag: "user",
		Content: []waBinary.Node{
			{Tag: "contact", Attrs: waBinary.Attrs{"type": "out"}},
			{Tag: "username", Content: "nobody"},
		},
	}
	got := parseUsernameUserNode(user)
	if got.Found {
		t.Fatalf("type=out should not be found: %+v", got)
	}
}

func TestParseUsernameUsyncUsersEmpty(t *testing.T) {
	list := waBinary.Node{Tag: "list"}
	if got := parseUsernameUsyncUsers(list); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstUsernameResultFromIQ(t *testing.T) {
	lid := types.NewJID("99", types.HiddenUserServer)
	resp := &waBinary.Node{
		Tag: "iq",
		Content: []waBinary.Node{{
			Tag: "usync",
			Content: []waBinary.Node{{
				Tag: "list",
				Content: []waBinary.Node{{
					Tag:   "user",
					Attrs: waBinary.Attrs{"jid": lid},
					Content: []waBinary.Node{
						{Tag: "username", Content: "dave"},
					},
				}},
			}},
		}},
	}
	got := firstUsernameResult(resp, "dave")
	if !got.Found || got.JID != lid.String() || got.Username != "dave" {
		t.Fatalf("got %+v", got)
	}

	empty := firstUsernameResult(&waBinary.Node{Tag: "iq"}, "eve")
	if empty.Found || empty.Username != "eve" {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestUsernameQueryNodes(t *testing.T) {
	nodes := usernameQueryNodes()
	if len(nodes) != 3 || nodes[0].Tag != "contact" || nodes[1].Tag != "username" || nodes[2].Tag != "lid" {
		t.Fatalf("query nodes = %+v", nodes)
	}
}
