package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/omarshahine/trakt-plugin/backup"
	"github.com/spf13/cobra"
)

func newBackupRestoreFavoritesCmd() *cobra.Command {
	c := &cobra.Command{Use: "restore-favorites", Short: "Review or restore missing favorites membership only; no ranks/notes/dates", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().String("source", "", "Desired archive ZIP")
	_ = c.MarkFlagRequired("source")
	c.Flags().String("current", "", "Current archive ZIP for reviewed membership batch")
	_ = c.MarkFlagRequired("current")
	c.Flags().Int("limit", 100, "Maximum favorites in this batch (1-100)")
	c.Flags().String("review-id", "", "Exact review_id from offline preview; required with --apply")
	c.Flags().String("expect-user", "", "Expected account slug; required with --apply")
	c.Flags().Bool("apply", false, "Execute reviewed additions; otherwise offline preview")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		source, _ := cmd.Flags().GetString("source")
		current, _ := cmd.Flags().GetString("current")
		limit, _ := cmd.Flags().GetInt("limit")
		apply, _ := cmd.Flags().GetBool("apply")
		digest, _ := cmd.Flags().GetString("review-id")
		expected, _ := cmd.Flags().GetString("expect-user")
		review, err := backup.PreviewFavoriteRestore(source, current, limit)
		if err != nil {
			return err
		}
		if !apply {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(review)
		}
		if digest == "" || digest != review.ReviewID {
			return fmt.Errorf("review-id missing or no longer matches archives/batch; preview again before applying")
		}
		if expected == "" || expected != review.Account.Slug {
			return fmt.Errorf("expect-user must match the reviewed current account slug")
		}
		client := api.NewAPIClient()
		result, applyErr := client.ApplyMissingFavorites(expected, review.Account, review.Favorites)
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			ReviewID   string   `json:"review_id"`
			Remaining  int      `json:"remaining"`
			Blocked    int      `json:"blocked"`
			OutOfScope []string `json:"out_of_scope"`
			api.FavoriteRestoreResult
		}{review.ReviewID, review.Remaining, len(review.Blocked), review.OutOfScope, result}); err != nil {
			return err
		}
		return applyErr
	}
	return c
}
func init() { backupCmd.AddCommand(newBackupRestoreFavoritesCmd()) }
