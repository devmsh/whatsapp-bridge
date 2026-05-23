package wa

import (
	"sync"

	"whatsapp-bridge-v2/internal/db"
)

// Broadcaster fans out incoming messages to registered listeners.
// Each listener is a channel that receives a copy of every new message.
// Listeners can optionally filter by chat JID (empty string = all chats).
type Broadcaster struct {
	mu        sync.RWMutex
	listeners map[chan *db.Message]string // channel → filter JID ("" = all)
}

// NewBroadcaster creates an empty Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		listeners: make(map[chan *db.Message]string),
	}
}

// Subscribe registers a new listener. filterJID limits delivery to one chat;
// pass "" to receive all messages. Returns a channel the caller reads from.
func (b *Broadcaster) Subscribe(filterJID string) chan *db.Message {
	ch := make(chan *db.Message, 32)
	b.mu.Lock()
	b.listeners[ch] = filterJID
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes the listener channel.
func (b *Broadcaster) Unsubscribe(ch chan *db.Message) {
	b.mu.Lock()
	delete(b.listeners, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish sends msg to all matching listeners (non-blocking — slow listeners are skipped).
func (b *Broadcaster) Publish(msg *db.Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch, jid := range b.listeners {
		if jid == "" || jid == msg.ChatJID {
			select {
			case ch <- msg:
			default: // listener too slow — drop rather than block
			}
		}
	}
}
