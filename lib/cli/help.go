package cli

import (
	"fmt"
)

const (
	tokensGroup = iota
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
		fmt.Println("Available commands:")
		fmt.Printf("%-16s  %s\n", "Command", "Description")
		fmt.Printf("%-16s  %s\n", "----------------", "------------------------------")
		for _, g := range commandHelp {
			if len(g) > 0 {
				for _, c := range g {
					fmt.Printf("%-16s  %s\n", c[0], c[1])
				}
				fmt.Printf("%-16s  %s\n", "----------------", "------------------------------")
			}
		}
		return nil
	}, nil
}
