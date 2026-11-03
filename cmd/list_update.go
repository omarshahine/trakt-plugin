package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newListUpdateCmd() *cobra.Command {
	c := &cobra.Command{Use: "update", Short: "Preview list name/description/privacy updates; apply requires expected account", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().String("list", "", "Canonical numeric LIST ID (stable across rename)")
	_ = c.MarkFlagRequired("list")
	c.Flags().String("name", "", "New name; omission preserves")
	c.Flags().String("description", "", "New description; explicitly empty clears, omission preserves")
	c.Flags().String("privacy", "", "private, link, friends or public; omission preserves")
	c.Flags().String("expect-user", "", "Expected account slug; required with apply")
	c.Flags().String("expect-updated-at", "", "Optional exact list updated_at revision guard")
	c.Flags().Bool("apply", false, "Execute authorized update; default offline preview")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		list, _ := cmd.Flags().GetString("list")
		revision, _ := cmd.Flags().GetString("expect-updated-at")
		x := api.ListUpdate{List: list, ExpectedUpdatedAt: revision}
		if cmd.Flags().Changed("name") {
			s, _ := cmd.Flags().GetString("name")
			x.Name = &s
		}
		if cmd.Flags().Changed("description") {
			s, _ := cmd.Flags().GetString("description")
			x.Description = &s
		}
		if cmd.Flags().Changed("privacy") {
			s, _ := cmd.Flags().GetString("privacy")
			x.Privacy = &s
		}
		preview, err := api.PreviewListUpdate("me", x)
		if err != nil {
			return err
		}
		apply, _ := cmd.Flags().GetBool("apply")
		if !apply {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(preview)
		}
		expected, _ := cmd.Flags().GetString("expect-user")
		if expected == "" {
			return fmt.Errorf("expect-user required with apply")
		}
		client := api.NewAPIClient()
		result, applyErr := client.ApplyListUpdate(expected, x)
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
			return err
		}
		return applyErr
	}
	return c
}
