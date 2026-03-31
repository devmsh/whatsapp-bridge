package db

// EventLog maps to the events_log table for structured change events.
type EventLog struct {
	ID        int64  `json:"id,omitempty"`
	EventType string `json:"event_type"`
	JID       string `json:"jid"`
	ActorJID  string `json:"actor_jid,omitempty"`
	Data      string `json:"data,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// StoreEventLog inserts an event log entry.
func (s *Store) StoreEventLog(e *EventLog) error {
	_, err := s.DB.Exec(`INSERT INTO events_log (event_type, jid, actor_jid, data, timestamp)
		VALUES (?,?,?,?,?)`,
		e.EventType, e.JID, e.ActorJID, e.Data, e.Timestamp,
	)
	return err
}

// GetEventLogs returns recent event log entries.
func (s *Store) GetEventLogs(limit int) ([]EventLog, error) {
	rows, err := s.DB.Query(
		`SELECT id, event_type, jid, actor_jid, data, timestamp
		 FROM events_log ORDER BY timestamp DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []EventLog
	for rows.Next() {
		var e EventLog
		if err := rows.Scan(&e.ID, &e.EventType, &e.JID, &e.ActorJID, &e.Data, &e.Timestamp); err != nil {
			return logs, err
		}
		logs = append(logs, e)
	}
	return logs, rows.Err()
}
