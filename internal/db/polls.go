package db

// Poll maps to the polls table.
type Poll struct {
	MessageID     string `json:"message_id"`
	ChatJID       string `json:"chat_jid"`
	Question      string `json:"question"`
	Options       string `json:"options"`
	MaxSelections int    `json:"max_selections"`
	CreatedAt     int64  `json:"created_at"`
}

// PollVote maps to the poll_votes table.
type PollVote struct {
	PollMessageID   string `json:"poll_message_id"`
	PollChatJID     string `json:"poll_chat_jid"`
	VoterJID        string `json:"voter_jid"`
	SelectedOptions string `json:"selected_options"`
	Timestamp       int64  `json:"timestamp"`
}

// StorePoll upserts a poll.
func (s *Store) StorePoll(p *Poll) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO polls (
		message_id, chat_jid, question, options, max_selections, created_at
	) VALUES (?,?,?,?,?,?)`,
		p.MessageID, p.ChatJID, p.Question, p.Options, p.MaxSelections, p.CreatedAt,
	)
	return err
}

// GetPoll returns a poll by message ID and chat JID.
func (s *Store) GetPoll(messageID, chatJID string) (*Poll, error) {
	row := s.DB.QueryRow(`SELECT message_id, chat_jid, question, options, max_selections, created_at
		FROM polls WHERE message_id = ? AND chat_jid = ?`, messageID, chatJID)
	p := &Poll{}
	err := row.Scan(&p.MessageID, &p.ChatJID, &p.Question, &p.Options, &p.MaxSelections, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// StorePollVote upserts a poll vote.
func (s *Store) StorePollVote(v *PollVote) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO poll_votes (
		poll_message_id, poll_chat_jid, voter_jid, selected_options, timestamp
	) VALUES (?,?,?,?,?)`,
		v.PollMessageID, v.PollChatJID, v.VoterJID, v.SelectedOptions, v.Timestamp,
	)
	return err
}

// GetPollVotes returns all votes for a poll.
func (s *Store) GetPollVotes(pollMessageID, pollChatJID string) ([]PollVote, error) {
	rows, err := s.DB.Query(
		`SELECT poll_message_id, poll_chat_jid, voter_jid, selected_options, timestamp
		 FROM poll_votes WHERE poll_message_id = ? AND poll_chat_jid = ?`,
		pollMessageID, pollChatJID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var votes []PollVote
	for rows.Next() {
		var v PollVote
		if err := rows.Scan(&v.PollMessageID, &v.PollChatJID, &v.VoterJID, &v.SelectedOptions, &v.Timestamp); err != nil {
			return votes, err
		}
		votes = append(votes, v)
	}
	return votes, rows.Err()
}
