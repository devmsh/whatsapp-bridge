package wa

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

// ensureAttempts dedupes EnsureBusinessProfile calls so a flurry of inbound
// messages from the same unknown business doesn't fan out into a flurry of
// GetUserInfo / GetBusinessProfile queries. Cleared on bridge restart — fine,
// because the resolved data also lives in the contacts table.
var ensureAttempts sync.Map // jid -> time.Time of last attempt

const ensureCooldown = time.Hour

// EnsureBusinessProfile resolves the WA-side identity for a phone-form DM JID
// (verified business name, plain business name, push name, and the
// is_business signal) and upserts it into the contacts table. It is safe to
// call repeatedly; a one-hour per-JID cooldown keeps it cheap.
//
// Returns the contact row after the refresh (or the cached one inside the
// cooldown window). Returns nil + nil error for JIDs that aren't DM phone
// JIDs — group / LID / broadcast / newsletter JIDs have no business profile.
func EnsureBusinessProfile(ctx context.Context, wa *whatsmeow.Client, store *db.Store, jidStr string) (*db.Contact, error) {
	if !strings.HasSuffix(jidStr, "@s.whatsapp.net") {
		return nil, nil
	}

	if v, ok := ensureAttempts.Load(jidStr); ok {
		if last, ok := v.(time.Time); ok && time.Since(last) < ensureCooldown {
			return store.GetContact(jidStr)
		}
	}
	ensureAttempts.Store(jidStr, time.Now())

	parsedJID, err := types.ParseJID(jidStr)
	if err != nil {
		return nil, err
	}

	var verifiedName, businessName, pushName string

	// 1) UserInfo carries the verified (green-check) business name when set.
	//    Cheap RPC; we only care about the VerifiedName branch.
	if infos, err := wa.GetUserInfo(ctx, []types.JID{parsedJID}); err == nil {
		if info, ok := infos[parsedJID]; ok && info.VerifiedName != nil && info.VerifiedName.Details != nil {
			verifiedName = info.VerifiedName.Details.GetVerifiedName()
		}
	}

	// 2) whatsmeow's contact store holds BusinessName / PushName from the
	//    WA roster (when it was ever synced). Local read, no RPC.
	if wa.Store != nil && wa.Store.Contacts != nil {
		if info, err := wa.Store.Contacts.GetContact(ctx, parsedJID); err == nil && info.Found {
			businessName = info.BusinessName
			pushName = info.PushName
		}
	}

	// 3) GetBusinessProfile confirms is_business and carries address /
	//    categories / hours. We only use the is_business signal here; the
	//    rest is left for a future surfacing cycle.
	isBusiness := false
	if profile, err := wa.GetBusinessProfile(ctx, parsedJID); err == nil && profile != nil {
		isBusiness = true
	}

	if verifiedName == "" && businessName == "" && pushName == "" && !isBusiness {
		// Nothing to record — leave whatever's in the contacts table alone.
		return store.GetContact(jidStr)
	}

	c := &db.Contact{
		JID:          jidStr,
		Phone:        parsedJID.User,
		PushName:     pushName,
		BusinessName: businessName,
		VerifiedName: verifiedName,
		IsBusiness:   isBusiness,
	}
	if err := store.StoreContact(c); err != nil {
		return nil, err
	}
	return store.GetContact(jidStr)
}
