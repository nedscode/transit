package inboxes

// Examples:
//    entry := foo.bar.baz:123
//    matches: foo.bar.baz:123
//             foo.bar.baz
//             foo
//             *
//    nomatch: foo.bar.baz:1234
//             foo.bar.baz:12
//             foo.bar.b
//             foo.baz
func topicMatch(entry, inbox string) bool {
	e := len(entry)
	i := len(inbox)

	// Check for wildcard.
	if i == 1 && inbox[0] == '*' {
		return true
	}

	// Next easiest case, the exact match...
	if e == i && entry == inbox {
		return true
	}

	// Cheap length comparison, if the entry is shorter than the inbox topic it can never match.
	if e <= i {
		return false
	}

	// Check the rune at entry[i], and make sure it's a sep char as we only match on whole segments.
	c := entry[i]
	if c != '.' && c != ':' {
		return false
	}

	// Its a whole segment match, so compare the prefix.
	return entry[0:i] == inbox
}
