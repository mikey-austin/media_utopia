package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Root().PersistentFlags().GetBool("json")
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Version string `json:"version"`
					Commit  string `json:"commit"`
					Date    string `json:"date"`
				}{
					Version: version,
					Commit:  commit,
					Date:    date,
				})
			}
			fmt.Printf("mu version %s (commit: %s, built: %s)\n", version, commit, date)
			return nil
		},
	}
}
