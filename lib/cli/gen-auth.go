package cli

import (
	"fmt"

	"google.golang.org/grpc"

	"github.com/nedscode/transit/proto"
)

func init() {
	addHelp(tokensGroup, "gen-auth", "Generate an auth token for the given public key")
}

func (c *Config) getToken(pk string) (tokenMaster, tokenRole, tokenName string) {
	ctx, cancel := c.timeout()
	defer cancel()

	masterKey := fmt.Sprintf("tokens/%s/master", pk)
	roleKey := fmt.Sprintf("tokens/%s/role", pk)
	nameKey := fmt.Sprintf("tokens/%s/name", pk)

	_, key := c.getPeersKey()
	res, err := c.node.ClusterGetKeys(
		ctx,
		&transit.Strings{
			Values: []string{masterKey, roleKey, nameKey},
		},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)
	if err != nil {
		return
	}

	keys := res.Values

	tokenMaster = keys[masterKey]
	tokenRole = keys[roleKey]
	tokenName = keys[nameKey]
	return
}

func (c *Config) genAuth(pk string) afterFunc {
	return func() error {
		tokenMaster, tokenRole, tokenName := c.getToken(pk)

		if tokenMaster != "" {
			auth, _, _ := transit.GetAuthTokenFor(tokenMaster, "")

			fmt.Printf("Auth token for %s master token %s [%q]:\n", tokenRole, tokenMaster, tokenName)
			fmt.Printf("  {public}-{hextime}-{nonce}-md5({private}-{hextime}-{nonce}) =\n")
			fmt.Printf("  %s\n", auth)
			return nil
		}

		return fmt.Errorf("could not find the matching master token for public key %s", pk)
	}
}

func (c *Config) genAuthCommand(args []string) (cb afterFunc, err error) {
	if len(args) < 1 {
		fmt.Println(`Usage: gen-auth {PUBLIC KEY}`)
		return
	}

	cb = c.withAnyPeer(c.genAuth(args[0]))
	return
}
