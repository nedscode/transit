package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/metadata"

	"github.com/nedscode/transit/proto"
)

type otp struct {
	mu        sync.Mutex
	curTime   int64
	curNonces []int64
	preNonces []int64
}

// This method is overly commented as it's fairly non-obvious what it's doing.
func (o *otp) ratchet(ts int64, nonce int64) bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	// The rule here is there's a set of nonces each second, and you can use them once in that 1 second window.
	// If the second rolls over (we see a newer nonce), you can still use a nonce generate in the prior second.
	// If you try to use a nonce older than 1 second from the time of the most recently seen nonce, it's invalid.
	// If you try to reuse a nonce for the current or previous second, it's invalid.

	// If the incoming nonce is > the current timestamp
	if ts > o.curTime {
		// But we're only one second past it
		if ts == o.curTime+1 {
			// Move the prior second's nonces into the previous list
			o.preNonces = o.curNonces
		} else {
			// Otherwise we have no previous nonces
			o.preNonces = nil
		}

		// Reset the current second's nonce list
		o.curNonces = nil

		// Time is now updated to the current nonce's time
		o.curTime = ts
	}

	// If the incoming nonce is older than the most recent minus one second
	if ts < o.curTime-1 {
		// Really? You took more than 2 seconds?
		// Sorry chum, should'a been faster.
		return false
	}

	var check *[]int64

	// If we're checking a nonce from the prior second
	if ts == o.curTime-1 {
		// Check for a match in the previous nonces
		check = &o.preNonces
	} else {
		// Else check the current nonces
		check = &o.curNonces
	}

	// Check over the nonces for a match
	for _, n := range *check {
		// If we find a matching nonce
		if n == nonce {
			// Uh, yeah, nice try mate
			return false
		}
	}

	// Add our nonce to the correct nonces list
	*check = append(*check, nonce)

	return true
}

type otpStore struct {
	mu    sync.RWMutex
	times map[string]*otp
}

func (o *otpStore) get(pub string) *otp {
	// Can we get the required item without a write lock?
	v := func() *otp {
		o.mu.RLock()
		defer o.mu.RUnlock()

		if t, ok := o.times[pub]; ok {
			// Yes!
			return t
		}

		// Doesn't exist :(
		return nil
	}()

	// No cigar...
	if v == nil {
		o.mu.Lock()
		defer o.mu.Unlock()

		// Check again to make sure it hasn't been created since we dropped the read lock and acquired the write.
		if t, ok := o.times[pub]; ok {
			// :/
			v = t
		} else {
			// No? Ok create a new one, and put it in the map.
			v = &otp{}
			o.times[pub] = v
		}
	}
	return v
}

func (b *Backend) getTokenFor(pk string) (master, role, name string) {
	master = b.store.Get(fmt.Sprintf("tokens/%s/master", pk))
	if master == "" {
		return
	}
	role = b.store.Get(fmt.Sprintf("tokens/%s/role", pk))
	name = b.store.Get(fmt.Sprintf("tokens/%s/name", pk))
	return
}

func tokensFromContext(ctx context.Context) (t []string) {
	mdi, _ := metadata.FromIncomingContext(ctx)
	mdo, _ := metadata.FromOutgoingContext(ctx)

	if v, ok := mdi["token"]; ok {
		t = append(t, v...)
	}
	if v, ok := mdo["token"]; ok {
		t = append(t, v...)
	}

	ai := mdi["grpcgateway-authorization"]
	ao := mdo["grpcgateway-authorization"]

	aaa := append(ai, ao...)

	for _, aa := range aaa {
		a := strings.Split(aa, " ")
		if strings.ToLower(a[0]) == "token" {
			t = append(t, a[1])
		}
	}

	return
}

func (b *Backend) requireCluster(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	b.logger.WithField("md", md).Info("metadata")

	tokens := tokensFromContext(ctx)
	if len(tokens) == 0 {
		return unauthenticatedError
	}
	clusterKey := b.store.Key()
	b.logger.WithField("cluster-key", clusterKey).WithField("tokens", tokens).Info("Got tokens")

	for _, k := range tokens {
		if len(k) == 48 && k == clusterKey {
			return nil
		}
	}
	return permissionError
}

func (b *Backend) requireRoles(ctx context.Context, roles ...string) (string, error) {
	tokenMeta := tokensFromContext(ctx)
	if len(tokenMeta) == 0 {
		if len(roles) == 0 {
			return "anonymous", nil
		}

		return "", unauthenticatedError
	}

	// Cluster key is automatically allowed to access everything.
	clusterKey := b.store.Key()
	for _, k := range tokenMeta {
		if len(k) == 48 && k == clusterKey {
			return "cluster", nil
		}
	}

	// Check each supplied key in order:
	invalid := false
	tested := false
	for _, ttt := range tokenMeta {
		tt := strings.Split(ttt, ",")
		for _, authToken := range tt {
			if authToken != "" {
				pk := authToken[0:12]

				tokenMaster, tokenRole, tokenName := b.getTokenFor(pk)
				if tokenMaster != "" {
					tested = true

					matches := false
					if len(roles) == 0 {
						matches = true
					} else {
						for _, r := range roles {
							if r == tokenRole {
								matches = true
								break
							}
						}
					}

					if matches {
						tokenParts := strings.Split(authToken, "-")
						if n := len(tokenParts); n == 4 {
							hexTime := tokenParts[1]
							hexNonce := tokenParts[2]
							compare, ts, nonce := transit.GetAuthTokenFor(tokenMaster, fmt.Sprintf("%s-%s", hexTime, hexNonce))

							if compare == authToken {
								// This is a valid Auth token, last thing is the replay protection check
								if b.otp.get(pk).ratchet(ts, nonce) {
									return tokenName, nil
								}
							}
						} else {
							invalid = true
						}
					}
				}
			}
		}
	}

	if len(roles) == 0 {
		return "anonymous", nil
	}

	if invalid {
		return "", invalidTokenError
	}

	if !tested {
		return "", unauthenticatedError
	}

	return "", permissionError
}
