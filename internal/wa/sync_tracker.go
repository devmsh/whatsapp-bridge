package wa

import (
	"sync"
	"time"
)

// SyncProgress is the snapshot the GUI renders during onboarding.
// Message/chat/contact counts are read live from the DB by the API layer.
// We deliberately do NOT expose WhatsApp's "progress percent": it reports 100%
// for the first (recent) batch while older batches are still arriving, which is
// misleading. Instead we report batch activity and let the UI show live counts.
type SyncProgress struct {
	HistoryBatches  int    `json:"history_batches"`   // history-sync batches processed so far
	LastSyncType    string `json:"last_sync_type,omitempty"`
	LastBatchAt     int64  `json:"last_batch_at"`     // unix time of the last history batch (0 = none)
	OfflineTotal    int    `json:"offline_total"`     // messages queued while offline (from preview)
	OfflineDone     int    `json:"offline_done"`      // delivered after reconnect
	InitialSyncDone bool   `json:"initial_sync_done"` // first group + contact refresh done
	UpdatedAt       int64  `json:"updated_at"`
}

// SyncTracker accumulates sync signals from whatsmeow events.
type SyncTracker struct {
	mu sync.RWMutex
	p  SyncProgress
}

// NewSyncTracker creates an empty tracker.
func NewSyncTracker() *SyncTracker {
	return &SyncTracker{p: SyncProgress{UpdatedAt: time.Now().Unix()}}
}

// Snapshot returns the current progress.
func (t *SyncTracker) Snapshot() SyncProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.p
}

func (t *SyncTracker) update(mutate func(*SyncProgress)) {
	t.mu.Lock()
	mutate(&t.p)
	t.p.UpdatedAt = time.Now().Unix()
	t.mu.Unlock()
}

// RecordHistory notes that a history-sync batch arrived.
func (t *SyncTracker) RecordHistory(syncType string) {
	now := time.Now().Unix()
	t.update(func(p *SyncProgress) {
		p.HistoryBatches++
		p.LastSyncType = syncType
		p.LastBatchAt = now
	})
}

// RecordOfflinePreview notes how many items the server queued while offline.
func (t *SyncTracker) RecordOfflinePreview(total int) {
	t.update(func(p *SyncProgress) { p.OfflineTotal = total })
}

// RecordOfflineCompleted notes that queued offline items have been delivered.
func (t *SyncTracker) RecordOfflineCompleted(count int) {
	t.update(func(p *SyncProgress) { p.OfflineDone = count })
}

// MarkInitialSyncDone records that the first group + contact refresh finished.
func (t *SyncTracker) MarkInitialSyncDone() {
	t.update(func(p *SyncProgress) { p.InitialSyncDone = true })
}
