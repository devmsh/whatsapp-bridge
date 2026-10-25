package extract_test

import (
	"testing"

	"whatsapp-bridge-v2/internal/extract"
)

// TestOwnerIgnoresAReplyToYourself — people reply to their own message to add
// a thought to what they just said. Measured on real chats, reading that as
// "you own this" made the person asking for the work its owner.
func TestOwnerIgnoresAReplyToYourself(t *testing.T) {
	nabil := "97450000101@s.whatsapp.net"
	first := line("A1", 100, "Nabil", "بخصوص التغطيات الاعلامية")
	first.SenderJID = nabil
	second := line("A2", 101, "Nabil", "نحتاج نضيف قياس لحجم التغطية ومصادرها")
	second.SenderJID = nabil
	second.ReplyTo = "A1"

	c := chunkWith(first, second)
	v, _, ok := extract.Verify(c, proposal("#A2", second.Text, "إضافة قياس للتغطية"))
	if !ok {
		t.Fatalf("the proposal should verify")
	}
	jid, src := extract.ResolveOwner(v, nil)
	if jid == nabil {
		t.Errorf("the person asking became the owner, from a reply to himself (source %q)", src)
	}
}
