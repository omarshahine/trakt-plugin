package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/omarshahine/trakt-plugin/backup"
	"github.com/spf13/cobra"
)

var backupPlanCmd = &cobra.Command{
	Use: "plan-restore", Short: "Preview additions from a backup; never writes to Trakt", Args: cobra.NoArgs, SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		source, _ := cmd.Flags().GetString("source")
		current, _ := cmd.Flags().GetString("current")
		live, _ := cmd.Flags().GetString("live-current")
		output, _ := cmd.Flags().GetString("import-json")
		if source == "" || (current == "") == (live == "") {
			return fmt.Errorf("source and exactly one of current or live-current are required")
		}
		if live != "" {
			// Validate the source before any network reads or snapshot creation.
			if _, err := backup.PlanRestore(source, source); err != nil {
				return err
			}
			client := api.NewAPIClient()
			user, err := client.GetUserSlug()
			if err != nil {
				return err
			}
			_, err = backup.Export(&client, backup.Options{User: user, Output: live, Limit: 250, Delay: time.Second, MaxPages: 10000, Progress: func(r backup.Resource) {
				if r.Pages%25 == 0 || r.Complete {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %d records, %d pages\n", r.Name, r.Records, r.Pages)
				}
			}})
			if err != nil {
				return err
			}
			current = live
		}
		plan, err := backup.PlanRestore(source, current)
		if err != nil {
			return err
		}
		if output != "" {
			if err := backup.WriteImportJSON(output, plan); err != nil {
				return err
			}
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Read-only restore plan: %d import candidates, %d conflicts, %d unsupported/coverage notices\n", len(plan.Candidates), len(plan.Conflicts), len(plan.Unsupported))
		if output != "" {
			fmt.Fprintln(cmd.OutOrStdout(), "Review-only Trakt website import JSON:", output)
		}
		for _, warning := range plan.Warnings {
			fmt.Fprintln(cmd.OutOrStdout(), "Note:", warning)
		}
		return nil
	},
}

func init() {
	backupCmd.AddCommand(backupPlanCmd)
	backupPlanCmd.Flags().String("source", "", "Backup ZIP containing the desired records")
	backupPlanCmd.Flags().String("current", "", "Validated current-account ZIP; offline mode, no authentication")
	backupPlanCmd.Flags().String("live-current", "", "Fetch a fresh read-only account snapshot to this NEW ZIP, then plan (paced requests)")
	backupPlanCmd.Flags().String("import-json", "", "Optional NEW file containing only conflict-free candidate rows in Trakt website import format; does not import")
}
