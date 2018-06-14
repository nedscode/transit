package cli

import (
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"github.com/norganna/style"

	"github.com/nedscode/transit/lib/server"
	"github.com/nedscode/transit/proto"
)

func init() {
	addHelp(tokensGroup, "add-token", "Add a new token with the given role and name")
}

func (c *Config) addTokenCommand(args []string) (cb afterFunc, err error) {
	if len(args) < 2 {
		style.Println(`Usage: add-token {ROLE} {NAME}`)
		return
	}

	role := args[0]
	if !server.AssignableRoles[role] {
		validRoles := strings.Join(server.AssignableRolesList, ", ")
		style.Printf("‹ec:Invalid role› %q must be one of: %s\n", role, validRoles)
		return
	}

	name := strings.Join(args[1:], " ")

	cb = c.withLeader(c.addToken(role, name))
	return
}

func (c *Config) addToken(role, name string) (cb afterFunc) {
	return func() error {
		tokenMaster := transit.NewToken()
		pk := tokenMaster[0:12]

		exist, _, _ := c.getToken(pk)

		if exist != "" {
			return style.Errorf("new token master public key already exists: %s", pk)
		}

		ctx, cancel := c.timeout()
		defer cancel()

		masterKey := fmt.Sprintf("tokens/%s/master", pk)
		roleKey := fmt.Sprintf("tokens/%s/role", pk)
		nameKey := fmt.Sprintf("tokens/%s/name", pk)

		_, key := c.getPeersKey()
		ret, err := c.node.ClusterApply(
			ctx,
			&transit.ApplyCommands{
				Commands: []*transit.Command{
					{Operation: "set", Key: masterKey, Value: tokenMaster},
					{Operation: "set", Key: roleKey, Value: role},
					{Operation: "set", Key: nameKey, Value: name},
				},
			},
			grpc.PerRPCCredentials(&transit.TokenCredentials{
				Token: key,
			}),
		)

		if err != nil {
			return err
		}

		if !ret.Succeed {
			return fmt.Errorf("error adding token: %s", ret.Error)
		}

		return nil
	}
}
