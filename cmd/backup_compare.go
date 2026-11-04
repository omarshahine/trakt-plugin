package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/backup"
	"github.com/spf13/cobra"
)

var backupCompareCmd = &cobra.Command{
	Use: "compare", Short: "Validate and compare two local backup ZIPs without contacting Trakt", Args: cobra.NoArgs, SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		before, _ := cmd.Flags().GetString("before")
		after, _ := cmd.Flags().GetString("after")
		limit, _ := cmd.Flags().GetInt("sample-limit")
		if before == "" || after == "" {
			return fmt.Errorf("before and after ZIP paths are required")
		}
		report, err := backup.Compare(before, after, limit)
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Compared validated archives: +%d / -%d / %d changed records in shared resources\n", report.Added, report.Removed, report.Changed)
		fmt.Fprintf(cmd.OutOrStdout(), "Changes: %d media-metadata-only, %d record-data, %d account snapshots\n", report.MetadataOnlyChanged, report.RecordDataChanged, report.SnapshotChanged)
		for _, r := range report.Resources {
			if r.Coverage != "both" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: coverage %s (not counted as additions/removals)\n", r.Name, r.Coverage)
			} else if *r.Added != 0 || *r.Removed != 0 || *r.Changed != 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: +%d / -%d / %d changed\n", r.Name, *r.Added, *r.Removed, *r.Changed)
			}
		}
		for _, warning := range report.Warnings {
			fmt.Fprintln(cmd.OutOrStdout(), "Note:", warning)
		}
		return nil
	},
}

func init() {
	backupCmd.AddCommand(backupCompareCmd)
	backupCompareCmd.Flags().String("before", "", "Earlier native backup or flat website-export ZIP")
	backupCompareCmd.Flags().String("after", "", "Later native backup or flat website-export ZIP")
	backupCompareCmd.Flags().Int("sample-limit", 10, "Maximum ID/field-path samples per change type and resource (0-100); never prints field values")
}
