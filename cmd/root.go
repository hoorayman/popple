// Package cmd is the package for all commands
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hoorayman/popple/cmd/server"
)

var rootCmd = &cobra.Command{
	Use: "popple",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(server.ServerCmd)
}
