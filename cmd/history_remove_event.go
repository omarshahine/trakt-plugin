package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newHistoryRemoveEventCmd() *cobra.Command {
	command := &cobra.Command{Use: "remove-event", Short: "Preview or explicitly remove one dated history event, never all plays", Args: cobra.NoArgs, SilenceUsage: true}
	for _, flag := range []string{"history-id", "type", "trakt-id", "watched-at"} {
		command.Flags().String(flag, "", "Exact event guard; required (type is movie or episode)")
		_ = command.MarkFlagRequired(flag)
	}
	command.Flags().Bool("apply", false, "Execute one event deletion after fresh account/event checks; default offline preview")
	command.Flags().String("expect-user", "", "Account slug required with --apply")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		historyID, _ := cmd.Flags().GetString("history-id")
		kind, _ := cmd.Flags().GetString("type")
		mediaID, _ := cmd.Flags().GetString("trakt-id")
		watchedAt, _ := cmd.Flags().GetString("watched-at")
		expected, _ := cmd.Flags().GetString("expect-user")
		apply, _ := cmd.Flags().GetBool("apply")
		target := api.HistoryEventRef{HistoryID: historyID, Type: kind, TraktID: mediaID, WatchedAt: watchedAt}
		preview, err := api.PreviewHistoryEventRemoval(target)
		if err != nil {
			return err
		}
		if !apply {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				api.HistoryEventRemovalPreview
				Expected string `json:"expected_account,omitempty"`
				Note     string `json:"note"`
			}{preview, expected, "Offline, unverified preview. No login/network or account changes. Apply requires matching fresh account, media, event ID and timestamp, then scoped readback; other account activity is not transactionally locked."})
		}
		if expected == "" {
			return fmt.Errorf("expect-user is required with apply")
		}
		client := api.NewAPIClient()
		result, applyErr := client.ApplyHistoryEventRemoval(expected, target)
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
			return err
		}
		return applyErr
	}
	return command
}

func init() { historyCmd.AddCommand(newHistoryRemoveEventCmd()) }
