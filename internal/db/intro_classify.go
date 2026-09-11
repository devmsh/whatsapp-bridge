package db

import (
	"strconv"
	"time"
)

// Auto-classification of intro chats.
//
// The Intro label is the single source of truth for "this is someone I have
// only just met". Two things write it: you, by hand, and this pass. The filter
// reads the label, so both kinds of decision show up in one place and either
// can be undone by removing the label.
//
// The important rule is the watermark. The first pass over history was reviewed
// together — some suggestions were taken, some were not. Re-running detection
// over those same chats would quietly re-apply a label that was deliberately
// left off, and there would be no way to make a rejection stick. So the
// classifier only ever looks at conversations that STARTED after the watermark,
// and never revisits what has already been judged.
const (
	introTagKey       = "intro_tag_id"
	introWatermarkKey = "intro_classified_since"
	introOwnNameKey   = "intro_own_name"
	// Only a clear match is applied without asking. Weaker candidates still
	// appear in the suggestion list, where a human decides.
	introAutoScore = 5
)

// IntroTagID returns the label id used for intro chats, or 0 when none is set.
func (s *Store) IntroTagID() int64 {
	v, _, _ := s.GetSyncState(introTagKey)
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

// SetIntroTagID records which label means "new introduction".
func (s *Store) SetIntroTagID(id int64) error {
	return s.PutSyncState(introTagKey, strconv.FormatInt(id, 10))
}

// IntroOwnName is your own display name, used to spot a name card carrying it
// — someone sending "Mohammed Shurrab / One Studio" back to you is confirming
// who they just saved.
func (s *Store) IntroOwnName() string {
	v, _, _ := s.GetSyncState(introOwnNameKey)
	return v
}

func (s *Store) SetIntroOwnName(name string) error {
	return s.PutSyncState(introOwnNameKey, name)
}

// IntroWatermark is the point after which a conversation counts as new. Chats
// that began before it were part of the reviewed backfill and are never
// auto-labelled again.
func (s *Store) IntroWatermark() int64 {
	v, _, _ := s.GetSyncState(introWatermarkKey)
	ts, _ := strconv.ParseInt(v, 10, 64)
	return ts
}

func (s *Store) SetIntroWatermark(ts int64) error {
	return s.PutSyncState(introWatermarkKey, strconv.FormatInt(ts, 10))
}

// AutoClassifyIntros labels conversations that began after the watermark and
// read unmistakably like an introduction. Returns the names it labelled.
//
// It is deliberately conservative: only high-confidence matches are applied,
// and a chat you have already removed the label from is not re-labelled,
// because its start predates the watermark the moment the watermark advances.
func (s *Store) AutoClassifyIntros() ([]string, error) {
	tagID := s.IntroTagID()
	if tagID == 0 {
		return nil, nil // no label configured — nothing to do
	}
	since := s.IntroWatermark()
	if since == 0 {
		// Never run. Start from now rather than sweeping all of history: the
		// backfill was reviewed by hand, and this must not undo that.
		now := time.Now().Unix()
		return nil, s.SetIntroWatermark(now)
	}

	// A generous message cap here: a brand-new chat has not had time to grow,
	// and the score is what actually decides.
	candidates, err := s.IntroChats(since, 40, tagID, s.IntroOwnName())
	if err != nil {
		return nil, err
	}

	var labelled []string
	for _, c := range candidates {
		if c.Tagged || c.Score < introAutoScore {
			continue
		}
		if err := s.AssignTag(c.JID, tagID); err != nil {
			continue
		}
		labelled = append(labelled, c.Name)
	}

	// Move the watermark past everything just considered, so no chat is judged
	// twice. Without this a conversation stays in scope for ever and taking its
	// label off would be undone on the next tick — the same rejection problem
	// the watermark exists to prevent, just for newer chats.
	//
	// An intro announces itself in its opening messages, and a chat is looked at
	// within minutes of starting, so one look is enough.
	if err := s.SetIntroWatermark(time.Now().Unix()); err != nil {
		return labelled, err
	}
	return labelled, nil
}

// IntroLabelledJIDs returns the chats carrying the intro label, in every
// identity form, so the chat list can filter on it.
//
// The LID/phone expansion matters: a label sits on whichever JID the contact
// row uses, while the conversation may be stored under the other form. Without
// both, a labelled person simply would not appear in their own filter.
func (s *Store) IntroLabelledJIDs() (map[string]bool, error) {
	tagID := s.IntroTagID()
	out := map[string]bool{}
	if tagID == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(`
		SELECT DISTINCT jid FROM (
		    SELECT ct.contact_jid AS jid FROM contact_tags ct WHERE ct.tag_id = ?1
		    UNION
		    SELECT c.jid FROM contacts c
		     WHERE c.jid != '' AND (c.jid IN (SELECT contact_jid FROM contact_tags WHERE tag_id = ?1)
		                         OR c.lid IN (SELECT contact_jid FROM contact_tags WHERE tag_id = ?1))
		    UNION
		    SELECT c.lid FROM contacts c
		     WHERE c.lid != '' AND (c.jid IN (SELECT contact_jid FROM contact_tags WHERE tag_id = ?1)
		                         OR c.lid IN (SELECT contact_jid FROM contact_tags WHERE tag_id = ?1))
		)`, tagID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil && jid != "" {
			out[jid] = true
		}
	}
	return out, rows.Err()
}
