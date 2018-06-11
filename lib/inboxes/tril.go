package inboxes

import "github.com/norganna/trit"

// tril (analogous as bool is to Boolean) is an alias to the trit package's Trilean value.
type tril = trit.Trilean

const (
	yes   = trit.Pos
	no    = trit.Neg
	maybe = trit.Nor
)
