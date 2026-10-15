package search

import "strings"

// maxTSQueryInputBytes caps the free-text input before tokenizing so a huge
// query can never produce an unbounded tsquery expression.
const maxTSQueryInputBytes = 256

// BuildTSQuery turns free text into a safe to_tsquery('simple', …) expression:
// [a-zA-Z0-9_]+ runs, lowercased, each wrapped as a quoted lexeme with a
// prefix flag, joined with ' & '. Returns "" when there are no usable tokens
// (callers then skip the content pass). Input is capped at 256 bytes before
// tokenizing to bound the expression size.
//
// Runs of only underscores are dropped: the 'simple' parser produces no
// lexeme for "_", so emitting "'_':*" would only create a query that can
// never match. The same parser splits underscored words into separate lexemes
// on both sides of @@ ("order_items" becomes the "order"/"items" lexemes), so
// a quoted run such as 'order_items':* still matches text containing that
// identifier.
//
// Phrase nuance: a quoted underscored run parses on the query side as an
// adjacency phrase — 'order_items':* becomes 'order':* <-> 'items':* — so the
// query "order_items" matches document text "order items" adjacent in that
// order, but not the reversed "items order". A space-separated query
// ("order items") becomes a non-adjacent ' & ' AND that matches either order.
//
// No escaping is needed after the split: emitted tokens contain only ASCII
// letters, digits, and underscores, and the single quotes delimit the whole
// lexeme.
func BuildTSQuery(q string) string {
	if len(q) > maxTSQueryInputBytes {
		q = q[:maxTSQueryInputBytes]
	}

	tokens := make([]string, 0, 8)
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		token := q[start:end]
		start = -1
		if !containsAlphanumeric(token) {
			return
		}
		tokens = append(tokens, "'"+strings.ToLower(token)+"':*")
	}
	for i := 0; i < len(q); i++ {
		if isTokenByte(q[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(q))

	return strings.Join(tokens, " & ")
}

// isTokenByte reports whether b belongs to an [a-zA-Z0-9_] run.
func isTokenByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_'
}

// containsAlphanumeric reports whether s holds at least one ASCII letter or
// digit, i.e. it is not a run of only underscores.
func containsAlphanumeric(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
			return true
		}
	}
	return false
}
