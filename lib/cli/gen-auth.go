package cli

import (
	"fmt"

	"google.golang.org/grpc"

	"strconv"

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

func (c *Config) genAuth(pk string, instance int32) afterFunc {
	return func() error {
		tokenMaster, tokenRole, tokenName := c.getToken(pk)

		if tokenMaster != "" {
			auth, _, _ := transit.GetAuthTokenFor(tokenMaster, "", instance)

			c.logger.Infof("Auth token for %s master token %s [%q]:", tokenRole, tokenMaster, tokenName)
			c.logger.Infof("  {public}-{hextime}-{nonce}-md5({private}-{hextime}-{nonce}) =")
			fmt.Println(auth)
			return nil
		}

		return fmt.Errorf("could not find the matching master token for public key %s", pk)
	}
}

func (c *Config) genAuthCommand(args []string) (cb afterFunc, err error) {
	if len(args) < 1 {
		fmt.Println(`Usage: gen-auth {PUBLIC KEY} [{INSTANCE}]`)
		return
	}

	var instance int32
	if len(args) > 1 {
		v, pErr := strconv.ParseInt(args[1], 10, 64)
		if pErr != nil {
			err = fmt.Errorf("unable to convert instance id into number")
			return
		}
		instance = int32(v)
		if instance < 0 {
			err = fmt.Errorf("instance must be within the range: 0 ≤ instance ≤ 2147483647")
			return
		}
	}

	cb = c.withAnyPeer(c.genAuth(args[0], instance))
	return
}
