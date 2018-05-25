package cli

import (
	"fmt"
	"strings"

	"google.golang.org/grpc"

	"github.com/nedscode/transit/proto"
)

func init() {
	addHelp(tokensGroup, "list-tokens", "List the issued api tokens")
}

func (c *Config) listTokens() error {
	fmt.Println("Tokens:")
	fmt.Printf("  %-12s  %-10s  %s\n", "Public Key", "Role", "Name")
	fmt.Printf("  %-12s  %-10s  %s\n", "------------", "----------", "----------------")

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
		tokenRole := tokenCfg[fmt.Sprintf("tokens/%s/role", pk)]
		tokenName := tokenCfg[fmt.Sprintf("tokens/%s/name", pk)]
		fmt.Printf("  %-12s  %-10s  %s\n", pk, tokenRole, tokenName)
	}
	return nil
}

func (c *Config) listTokensCommand(args []string) (cb afterFunc, err error) {
	cb = c.withAnyPeer(c.listTokens)
	return
}
