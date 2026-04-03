package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hostoria-node",
	Short: "Hostoria Node — game server management daemon",
	Long: `Hostoria Node is the compute daemon that runs on VPS nodes.
It manages game server containers via Docker and exposes a REST/WebSocket API
that the Hostoria Billing Panel uses to provision and control game servers.`,
}

// Execute is the entry point called by main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(configureCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(versionCmd)
}

// versionCmd prints the binary version.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Hostoria Node %s\n", Version)
	},
}

// Version is set at build time via -ldflags "-X github.com/hostoria/hostoria-node/cmd.Version=x.y.z"
var Version = "dev"
