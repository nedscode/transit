package transit

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nedscode/transit/lib/secret"
)

// TokenCredentials is usable with grpc.WithPerRPCCredentials.
type TokenCredentials struct {
	Token string
}

// NewToken returns a token string suitable for use as a master token.
func NewToken() string {
	b := secret.Bytes(33)
	b[12] = '-'
	return string(b)
}

// GetAuthTokenFor returns a request token for the given token and timestamp string.
// If timestamp is not provided, one will be generated.
// If timestamp is provided but is invalid, or more than 5 minutes different from current time, an empty string will
// be returned.
func GetAuthTokenFor(token string, entropy string) (auth string, ts, nonce int64) {
	if len(token) == 48 {
		// This is a cluster token
		auth = token
		return
	}

	if len(token) < 14 || token[12] != '-' {
		// This is not a valid token
		// This is not a valid token
		return
	}

	var err error

	// Create a valid auth token for the provided master token
	now := time.Now().Unix()
	var hexTime, hexNonce string

	if entropy == "" {
		ts = now
		nonce = time.Now().UnixNano() % 1000000000

		hexTime = strconv.FormatInt(ts, 16)
		hexNonce = strconv.FormatInt(nonce, 16)
		entropy = fmt.Sprintf("%s-%s", hexTime, hexNonce)
	} else {
		ee := strings.Split(entropy, "-")
		if len(ee) != 2 {
			return "", 0, 0
		}

		hexTime = ee[0]
		hexNonce = ee[1]

		ts, err = strconv.ParseInt(hexTime, 16, 64)
		if err != nil {
			return
		}

		nonce, err = strconv.ParseInt(hexNonce, 16, 64)
		if err != nil {
			return
		}

		// Allow a 30 second window around "current time" to allow for clock drift
		if delta := now - ts; delta < -15 || delta > 15 {
			return
		}
	}

	public := token[0:12]
	private := token[13:]
	sum := md5.Sum([]byte(fmt.Sprintf("%s-%s", private, entropy)))
	checksum := hex.EncodeToString(sum[:])

	auth = fmt.Sprintf("%s-%s-%s", public, entropy, checksum)
	return
}

// GetRequestMetadata gets the request's metadata, including a current token.
func (t *TokenCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	md := map[string]string{}
	if t.Token != "" {
		md["token"], _, _ = GetAuthTokenFor(t.Token, "")
	}

	return md, nil
}

// RequireTransportSecurity indicates whether the credentials requires transport security.
func (t *TokenCredentials) RequireTransportSecurity() bool {
	return false
}
