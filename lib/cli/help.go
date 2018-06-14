package cli

import (
	"github.com/norganna/style"
)

const (
	tokensGroup = iota
	serverGroup
	queuesGroup
	helpGroup
	groupCount
)

var commandHelp [groupCount][][2]string

func addHelp(group int, command, help string) {
	commandHelp[group] = append(commandHelp[group], [2]string{command, help})
}

func init() {
	addHelp(helpGroup, "help", "View available commands")
}

func (c *Config) helpCommand(_ []string) (afterFunc, error) {
	return func() error {
		style.Println("‹bc:Available commands:›\n")
		style.Printlnf("  ‹dh:16:|%s›  ‹dh:30:|%s›", "Command", "Description")

		for _, g := range commandHelp {
			if len(g) > 0 {
				for _, c := range g {
					style.Printf("  ‹bc:%-16s›  %s\n", c[0], c[1])
				}
				style.Printf("  ‹hl:16›  ‹hl:30›\n")
			}
		}
		return nil
	}, nil
}
