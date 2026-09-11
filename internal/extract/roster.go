package extract

import (
	"database/sql"
	"strings"

	"whatsapp-bridge-v2/internal/db"
)

// Who is in the conversation, and what the user knows about them.
//
// The kunya and how-we-met notes earn their place here. A model that knows
// "أبو يمان" is Mohammed Qudaih can resolve an owner from how people actually
// address each other, which is most of the time in these chats.

// Roster returns the people in a chat, the user's own display name, and
// whether the chat is a group.
func Roster(store *db.Store, chatJID string) ([]RosterPerson, string, bool, error) {
	own, _, _ := store.GetSyncState("intro_own_name")
	if own == "" {
		own = "Me"
	}
	isGroup := strings.HasSuffix(chatJID, "@g.us")
	if isGroup {
		people, err := groupRoster(store, chatJID)
		return people, own, true, err
	}
	people, err := dmRoster(store, chatJID)
	return people, own, false, err
}

func groupRoster(store *db.Store, chatJID string) ([]RosterPerson, error) {
	rows, err := store.DB.Query(`
		SELECT COALESCE(NULLIF(c.jid,''), p.jid) AS jid,
		       COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''),
		                NULLIF(c.business_name,''), NULLIF(p.display_name,''), '') AS name,
		       COALESCE(c.kunya, ''), COALESCE(c.how_we_met, ''),
		       COALESCE(p.is_admin, 0) OR COALESCE(p.is_super_admin, 0)
		FROM group_participants p
		LEFT JOIN contacts c ON (c.jid = p.jid OR c.lid = p.jid
		                         OR (p.phone != '' AND c.phone = p.phone))
		WHERE p.group_jid = ?`, chatJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRoster(rows)
}

func dmRoster(store *db.Store, chatJID string) ([]RosterPerson, error) {
	rows, err := store.DB.Query(`
		SELECT COALESCE(NULLIF(c.jid,''), ?) AS jid,
		       COALESCE(NULLIF(c.name,''), NULLIF(c.push_name,''),
		                NULLIF(c.business_name,''), '') AS name,
		       COALESCE(c.kunya, ''), COALESCE(c.how_we_met, ''), 0
		FROM contacts c
		WHERE c.jid = ?1 OR c.lid = ?1
		LIMIT 1`, chatJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRoster(rows)
}

func scanRoster(rows *sql.Rows) ([]RosterPerson, error) {
	out := []RosterPerson{}
	seen := map[string]bool{}
	for rows.Next() {
		var p RosterPerson
		var admin int
		if err := rows.Scan(&p.JID, &p.Name, &p.Kunya, &p.HowWeMet, &admin); err != nil {
			continue
		}
		p.IsAdmin = admin == 1
		if p.JID == "" || seen[p.JID] {
			continue
		}
		seen[p.JID] = true
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenderRoster is the block the model sees above the chat. Only what helps it
// name an owner: who is here, what they are called, and — where the user has
// written it down — why they are in this conversation at all.
func RenderRoster(people []RosterPerson, ownName string) string {
	var b strings.Builder
	b.WriteString("You are reading a chat on behalf of " + ownName + ".\n")
	if len(people) == 0 {
		return b.String()
	}
	b.WriteString("People in this conversation:\n")
	for _, p := range people {
		if strings.TrimSpace(p.Name) == "" && p.Kunya == "" {
			continue
		}
		b.WriteString("- " + firstNonEmpty(p.Name, p.Kunya))
		if p.Kunya != "" && p.Kunya != p.Name {
			b.WriteString(" (also called " + p.Kunya + ")")
		}
		if p.IsAdmin {
			b.WriteString(" [admin]")
		}
		if p.HowWeMet != "" {
			b.WriteString(" — " + firstRunes(p.HowWeMet, 90))
		}
		b.WriteString("\n")
	}
	return b.String()
}
