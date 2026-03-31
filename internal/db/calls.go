package db

// CallEvent maps to the calls table, derived from types.BasicCallMeta.
type CallEvent struct {
	CallID         string `json:"call_id"`
	FromJID        string `json:"from_jid"`
	Timestamp      int64  `json:"timestamp"`
	CallCreator    string `json:"call_creator,omitempty"`
	GroupJID       string `json:"group_jid,omitempty"`
	EventType      string `json:"event_type"`
	RemotePlatform string `json:"remote_platform,omitempty"`
	RemoteVersion  string `json:"remote_version,omitempty"`
	Data           string `json:"data,omitempty"`
}

// StoreCall upserts a call event.
func (s *Store) StoreCall(c *CallEvent) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO calls (
		call_id, from_jid, timestamp, call_creator, group_jid,
		event_type, remote_platform, remote_version, data
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		c.CallID, c.FromJID, c.Timestamp, c.CallCreator, c.GroupJID,
		c.EventType, c.RemotePlatform, c.RemoteVersion, c.Data,
	)
	return err
}

// GetCalls returns recent call events.
func (s *Store) GetCalls(limit int) ([]CallEvent, error) {
	rows, err := s.DB.Query(
		`SELECT call_id, from_jid, timestamp, call_creator, group_jid,
		 event_type, remote_platform, remote_version, data
		 FROM calls ORDER BY timestamp DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var calls []CallEvent
	for rows.Next() {
		var c CallEvent
		if err := rows.Scan(&c.CallID, &c.FromJID, &c.Timestamp, &c.CallCreator, &c.GroupJID,
			&c.EventType, &c.RemotePlatform, &c.RemoteVersion, &c.Data); err != nil {
			return calls, err
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}
