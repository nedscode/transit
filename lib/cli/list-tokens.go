package cli

import (
	"strings"

	"google.golang.org/grpc"
	"github.com/norganna/style"

	"github.com/nedscode/transit/proto"
	"path"
)

func init() {
	addHelp(tokensGroup, "list-tokens", "List the issued api tokens")
}

func (c *Config) listTokens() error {
	style.Println("‹hc:Tokens:›\n")
	style.Printlnf("  ‹dh:12:|%s›  ‹dh:10:|%s›  ‹dh:30:|%s›", "Public Key", "Role", "Name")

	pkk := map[string]bool{}
	ctx, cancel := c.timeout()
	defer cancel()

	_, key := c.getPeersKey()
	ret, err := c.node.ClusterList(
		ctx,
		&transit.String{Value: "tokens/"},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)
	if err != nil {
		return err
	}

	tokenCfg := ret.Values
	for k := range tokenCfg {
		kk := strings.Split(k, "/")
		pkk[kk[1]] = true
	}

	for pk := range pkk {
		tokenRole := tokenCfg[path.Join("tokens", pk, "role")]
		tokenName := tokenCfg[path.Join("tokens", pk, "name")]
		style.Printf("  ‹b:%-12s›  %-10s  %s\n", pk, tokenRole, tokenName)
	}
	return nil
}

func (c *Config) listTokensCommand(args []string) (cb afterFunc, err error) {
	cb = c.withAnyPeer(c.listTokens)
	return
}
