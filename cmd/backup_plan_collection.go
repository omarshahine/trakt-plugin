package cmd

import (
	"encoding/json"
	"github.com/omarshahine/trakt-plugin/backup"
	"github.com/spf13/cobra"
)

func newBackupPlanCollectionCmd() *cobra.Command {
	c := &cobra.Command{Use: "plan-collection", Short: "Inspect missing collection entries and date/metadata conflicts offline; no writes", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().String("source", "", "Desired archive ZIP")
	_ = c.MarkFlagRequired("source")
	c.Flags().String("current", "", "Current archive ZIP")
	_ = c.MarkFlagRequired("current")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		source, _ := cmd.Flags().GetString("source")
		current, _ := cmd.Flags().GetString("current")
		r, err := backup.PlanCollectionRestore(source, current)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
	}
	return c
}
func init() { backupCmd.AddCommand(newBackupPlanCollectionCmd()) }
