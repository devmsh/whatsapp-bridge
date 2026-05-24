package wa

import "encoding/json"

// mediaPolicyKey is the sync_state key where GUI changes are persisted.
const mediaPolicyKey = "media_policy"

// MediaPolicy decides which media auto-downloads on receipt. Per-type toggles
// plus an optional size cap. A disabled type or an oversize file is still
// recorded (type, mime, size, caption) — only the file bytes are skipped.
type MediaPolicy struct {
	Images    bool `json:"images"`
	Video     bool `json:"video"`
	Audio     bool `json:"audio"` // voice notes + audio
	Documents bool `json:"documents"`
	Stickers  bool `json:"stickers"`
	MaxSizeMB int  `json:"max_size_mb"` // 0 = no limit
}

// DefaultMediaPolicy downloads everything with no size cap (the original behavior).
func DefaultMediaPolicy() MediaPolicy {
	return MediaPolicy{Images: true, Video: true, Audio: true, Documents: true, Stickers: true, MaxSizeMB: 0}
}

// allows reports whether the given media type should be downloaded.
func (p MediaPolicy) allows(mediaType string) bool {
	switch mediaType {
	case "image":
		return p.Images
	case "video":
		return p.Video
	case "audio", "voice_note":
		return p.Audio
	case "document":
		return p.Documents
	case "sticker":
		return p.Stickers
	default:
		return true
	}
}

// maxBytes returns the size cap in bytes, or 0 for no limit.
func (p MediaPolicy) maxBytes() uint64 {
	if p.MaxSizeMB <= 0 {
		return 0
	}
	return uint64(p.MaxSizeMB) * 1024 * 1024
}

// MediaPolicy returns the current policy.
func (c *Client) MediaPolicy() MediaPolicy {
	c.policyMu.RLock()
	defer c.policyMu.RUnlock()
	return c.mediaPolicy
}

// SetMediaPolicy updates the policy in memory and persists it to the DB so it
// survives restarts. Applies immediately to newly received messages.
func (c *Client) SetMediaPolicy(p MediaPolicy) error {
	c.policyMu.Lock()
	c.mediaPolicy = p
	c.policyMu.Unlock()
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return c.Store.PutSyncState(mediaPolicyKey, string(b))
}

// InitMediaPolicy sets the startup default, then applies any persisted GUI
// override from the DB (which wins).
func (c *Client) InitMediaPolicy(def MediaPolicy) {
	p := def
	if v, _, _ := c.Store.GetSyncState(mediaPolicyKey); v != "" {
		var saved MediaPolicy
		if json.Unmarshal([]byte(v), &saved) == nil {
			p = saved
		}
	}
	c.policyMu.Lock()
	c.mediaPolicy = p
	c.policyMu.Unlock()
}
