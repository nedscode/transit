package secret

import (
	"math/rand"
	"time"
)

const secretChars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var secretSrc = rand.NewSource(time.Now().UnixNano())

// Bytes creates a new secret byte slice of given length
func Bytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = secretChars[secretSrc.Int63()%int64(len(secretChars))]
	}
	return b
}

// String creates a new secret string of given length
func String(n int) string {
	return string(Bytes(n))
}
