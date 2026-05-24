package mcp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// ── Tool definitions ────────────────────────────────────────────────

func toolCircleInfo() mcp.Tool {
	return mcp.NewTool("wa_circle_info",
		mcp.WithDescription("Get a Circle's full makeup for cross-chat task extraction: its purpose description, keywords, notes, every chat inside it (groups + DMs, including nested sub-circles, with each chat's purpose description and message count), and the list of sub-circles. Read this FIRST when extracting tasks for a circle — it tells you which chats to scan and what each one is about. Chats are sorted most-active first."),
		mcp.WithNumber("circle_id", mcp.Required(), mcp.Description("Numeric circle id")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)}),
	)
}

func toolListCircles() mcp.Tool {
	return mcp.NewTool("wa_list_circles",
		mcp.WithDescription("List ALL circles with their purpose description and keywords. Use this to understand the OTHER circles in the system so you can tell apart, in a DM with someone who belongs to several circles, which messages belong to the circle you are analyzing versus a different circle."),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)}),
	)
}

func toolGetProfile() mcp.Tool {
	return mcp.NewTool("wa_get_profile",
		mcp.WithDescription("Get the purpose description (profile) for one entity: a group, a contact/DM, or a circle. Use it to learn what a chat or person is about before judging whether a message belongs to the circle under analysis."),
		mcp.WithString("entity_type", mcp.Required(), mcp.Description("circle | group | contact")),
		mcp.WithString("ref", mcp.Required(), mcp.Description("JID for group/contact; numeric circle id for circle")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)}),
	)
}

// ── Handlers ────────────────────────────────────────────────────────

func (s *Server) profileDesc(entityType, ref string) string {
	var desc, status string
	s.db.QueryRow(`SELECT description, status FROM entity_profiles WHERE entity_type = ? AND entity_ref = ?`,
		entityType, ref).Scan(&desc, &status)
	if status == "empty" {
		return ""
	}
	return desc
}

func (s *Server) msgCount(jid string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_jid = ?`, jid).Scan(&n)
	return n
}

func (s *Server) groupNameMCP(jid string) string {
	var name string
	s.db.QueryRow(`SELECT name FROM groups WHERE jid = ?`, jid).Scan(&name)
	if name == "" {
		return jid
	}
	return name
}

func (s *Server) contactNameMCP(jid string) string {
	var name, push, biz string
	s.db.QueryRow(`SELECT name, push_name, business_name FROM contacts WHERE jid = ?`, jid).Scan(&name, &push, &biz)
	for _, v := range []string{name, biz, push} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return jid
}

// circleClosure returns all groups, contacts, and sub-circle ids reachable from
// root by descending nested circles (loop-safe; root excluded from sub-circles).
func (s *Server) circleClosure(root int64) (groups, contacts []string, subCircleIDs []int64) {
	seenG := map[string]bool{}
	seenC := map[string]bool{}
	visited := map[int64]bool{}
	stack := []int64{root}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if cur != root {
			subCircleIDs = append(subCircleIDs, cur)
		}
		type edge struct{ t, r string }
		var edges []edge
		rows, err := s.db.Query(`SELECT member_type, member_ref FROM circle_members WHERE circle_id = ?`, cur)
		if err != nil {
			continue
		}
		for rows.Next() {
			var mt, ref string
			if rows.Scan(&mt, &ref) == nil {
				edges = append(edges, edge{mt, ref})
			}
		}
		rows.Close()
		for _, e := range edges {
			switch e.t {
			case "group":
				if !seenG[e.r] {
					seenG[e.r] = true
					groups = append(groups, e.r)
				}
			case "contact":
				if !seenC[e.r] {
					seenC[e.r] = true
					contacts = append(contacts, e.r)
				}
			case "circle":
				if cid, err := strconv.ParseInt(e.r, 10, 64); err == nil {
					stack = append(stack, cid)
				}
			}
		}
	}
	return
}

type circleChat struct {
	Type         string `json:"type"`
	JID          string `json:"jid"`
	Name         string `json:"name"`
	MessageCount int    `json:"message_count"`
	Description  string `json:"description,omitempty"`
}

func (s *Server) handleCircleInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	idF, ok := args["circle_id"].(float64)
	if !ok || idF <= 0 {
		return mcp.NewToolResultError("circle_id is required"), nil
	}
	id := int64(idF)

	var name, notes, keywords string
	err := s.db.QueryRow(`SELECT name, notes, keywords FROM circles WHERE id = ?`, id).Scan(&name, &notes, &keywords)
	if err != nil {
		return mcp.NewToolResultError("circle not found"), nil
	}
	var kw []string
	if keywords != "" {
		json.Unmarshal([]byte(keywords), &kw)
	}

	groups, contacts, subIDs := s.circleClosure(id)

	chats := make([]circleChat, 0, len(groups)+len(contacts))
	for _, g := range groups {
		chats = append(chats, circleChat{
			Type: "group", JID: g, Name: s.groupNameMCP(g),
			MessageCount: s.msgCount(g), Description: s.profileDesc("group", g),
		})
	}
	for _, c := range contacts {
		chats = append(chats, circleChat{
			Type: "contact", JID: c, Name: s.contactNameMCP(c),
			MessageCount: s.msgCount(c), Description: s.profileDesc("contact", c),
		})
	}
	sort.SliceStable(chats, func(i, j int) bool { return chats[i].MessageCount > chats[j].MessageCount })

	type subCircle struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	subs := make([]subCircle, 0, len(subIDs))
	for _, sid := range subIDs {
		var sn string
		s.db.QueryRow(`SELECT name FROM circles WHERE id = ?`, sid).Scan(&sn)
		subs = append(subs, subCircle{ID: sid, Name: sn, Description: s.profileDesc("circle", strconv.FormatInt(sid, 10))})
	}

	out := map[string]any{
		"circle": map[string]any{
			"id":          id,
			"name":        name,
			"keywords":    kw,
			"notes":       notes,
			"description": s.profileDesc("circle", strconv.FormatInt(id, 10)),
		},
		"sub_circles": subs,
		"chats":       chats,
		"chat_count":  len(chats),
	}
	b, _ := json.Marshal(out)
	return mcp.NewToolResultText(string(b)), nil
}

func (s *Server) handleListCircles(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rows, err := s.db.Query(`SELECT id, name, keywords FROM circles ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return mcp.NewToolResultError("query failed"), nil
	}
	defer rows.Close()
	type circ struct {
		ID          int64    `json:"id"`
		Name        string   `json:"name"`
		Keywords    []string `json:"keywords,omitempty"`
		Description string   `json:"description,omitempty"`
	}
	out := []circ{}
	for rows.Next() {
		var id int64
		var name, kw string
		if rows.Scan(&id, &name, &kw) != nil {
			continue
		}
		var keywords []string
		if kw != "" {
			json.Unmarshal([]byte(kw), &keywords)
		}
		out = append(out, circ{ID: id, Name: name, Keywords: keywords, Description: s.profileDesc("circle", strconv.FormatInt(id, 10))})
	}
	b, _ := json.Marshal(map[string]any{"count": len(out), "circles": out})
	return mcp.NewToolResultText(string(b)), nil
}

func (s *Server) handleGetProfile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	entityType, _ := args["entity_type"].(string)
	ref, _ := args["ref"].(string)
	if entityType == "" || ref == "" {
		return mcp.NewToolResultError("entity_type and ref are required"), nil
	}
	var desc, source, status string
	err := s.db.QueryRow(`SELECT description, source, status FROM entity_profiles WHERE entity_type = ? AND entity_ref = ?`,
		entityType, ref).Scan(&desc, &source, &status)
	if err != nil {
		b, _ := json.Marshal(map[string]any{"entity_type": entityType, "ref": ref, "description": "", "status": "none"})
		return mcp.NewToolResultText(string(b)), nil
	}
	b, _ := json.Marshal(map[string]any{
		"entity_type": entityType, "ref": ref, "description": desc, "source": source, "status": status,
	})
	return mcp.NewToolResultText(string(b)), nil
}
