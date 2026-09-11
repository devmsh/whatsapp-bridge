package db

// LooksLikeNameCardForTest exposes the name-card rule to the db_test package.
// The rule is the sharp edge of intro detection — too loose and every "وعليكم
// السلام" becomes an introduction — so it is worth testing directly rather
// than only through the query that uses it.
func LooksLikeNameCardForTest(text, contactName, ownName string) bool {
	return looksLikeNameCard(text, contactName, ownName)
}
