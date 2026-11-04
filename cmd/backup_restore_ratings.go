package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/omarshahine/trakt-plugin/backup"
	"github.com/spf13/cobra"
)

func newBackupRestoreRatingsCmd() *cobra.Command {
	c := &cobra.Command{Use: "restore-ratings", Short: "Review or explicitly restore missing dated ratings only", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().String("source", "", "Desired archive ZIP")
	_ = c.MarkFlagRequired("source")
	c.Flags().String("current", "", "Current archive ZIP used to build the reviewed batch")
	_ = c.MarkFlagRequired("current")
	c.Flags().Int("limit", 100, "Maximum ratings selected in this batch (1-100)")
	c.Flags().String("review-id", "", "Exact review_id from offline preview; required with --apply")
	c.Flags().String("expect-user", "", "Expected authenticated account slug; required with --apply")
	c.Flags().Bool("apply", false, "Execute this reviewed ratings-only batch; otherwise offline preview")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		source, _ := cmd.Flags().GetString("source")
		current, _ := cmd.Flags().GetString("current")
		limit, _ := cmd.Flags().GetInt("limit")
		apply, _ := cmd.Flags().GetBool("apply")
		digest, _ := cmd.Flags().GetString("review-id")
		expected, _ := cmd.Flags().GetString("expect-user")
		review, err := backup.PreviewRatingRestore(source, current, limit)
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
		result, applyErr := client.ApplyMissingRatings(expected, review.Account, review.Ratings)
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			ReviewID  string `json:"review_id"`
			Remaining int    `json:"remaining"`
			Blocked   int    `json:"blocked"`
			api.RatingRestoreResult
		}{review.ReviewID, review.Remaining, len(review.Blocked), result}); err != nil {
			return err
		}
		return applyErr
	}
	return c
}
func init() { backupCmd.AddCommand(newBackupRestoreRatingsCmd()) }
