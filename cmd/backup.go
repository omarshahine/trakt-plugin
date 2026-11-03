package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/omarshahine/trakt-plugin/backup"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use: "backup", SilenceUsage: true, Short: "Export raw Trakt account data to a new verified ZIP",
	Long: "Read-only Trakt export covering website account-export categories with pagination, raw account snapshots, counts, checksums and an explicit completeness manifest. API category coverage does not guarantee identical website fields or a transactional snapshot. Never overwrites files. Failed exports retain partial data and exit nonzero.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		output, _ := cmd.Flags().GetString("output")
		limit, _ := cmd.Flags().GetInt("limit")
		delay, _ := cmd.Flags().GetInt("delay-ms")
		maxPages, _ := cmd.Flags().GetInt("max-pages")
		if output == "" || limit < 1 || limit > 1000 || delay < 0 || maxPages < 1 {
			return fmt.Errorf("output required; limit 1-1000, nonnegative delay-ms and positive max-pages required")
		}
		client := api.NewAPIClient()
		user, err := client.GetUserSlug()
		if err != nil {
			return fmt.Errorf("resolve trakt user: %w", err)
		}
		manifest, exportErr := backup.Export(&client, backup.Options{User: user, Output: output, Limit: limit, Delay: time.Duration(delay) * time.Millisecond, MaxPages: maxPages, Progress: func(r backup.Resource) {
			if r.Pages%25 == 0 || r.Complete {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: %d/%d records, %d pages\n", r.Name, r.Records, r.Expected, r.Pages)
			}
		}})
		if jsonOutput {
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Output string `json:"output"`
				backup.Manifest
			}{output, manifest}); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Backup: %s\nComplete (scoped): %t\n", output, manifest.Complete)
		}
		return exportErr
	},
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.Flags().String("output", "", "New ZIP path (required; no overwrite)")
	_ = backupCmd.MarkFlagRequired("output")
	backupCmd.Flags().Int("limit", 1000, "Records per API page (1-1000)")
	backupCmd.Flags().Int("delay-ms", 1000, "Minimum delay between API requests")
	backupCmd.Flags().Int("max-pages", 10000, "Maximum pages per resource; incomplete if reached")
}
