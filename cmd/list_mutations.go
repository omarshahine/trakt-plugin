package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func addListMutationCommands(parent *cobra.Command) {
	parent.AddCommand(newListUpdateCmd())
	for _, action := range []string{"create", "add", "remove", "notes", "reorder"} {
		action := action
		cmd := &cobra.Command{Use: action, Short: "Preview custom-list " + action + "; --apply explicitly writes to Trakt", Args: cobra.NoArgs, SilenceUsage: true}
		cmd.Flags().Bool("apply", false, "Explicitly execute this write against the authenticated account; default is offline preview")
		switch action {
		case "create":
			cmd.Flags().String("name", "", "New list name (required)")
			_ = cmd.MarkFlagRequired("name")
			cmd.Flags().String("description", "", "List description")
			cmd.Flags().String("privacy", "private", "private, link, friends or public; defaults private")
		case "add", "remove":
			cmd.Flags().String("type", "", "movie, show, season or episode (required)")
			_ = cmd.MarkFlagRequired("type")
			cmd.Flags().StringSlice("ids", nil, "Trakt MEDIA IDs, comma-separated (required)")
			_ = cmd.MarkFlagRequired("ids")
		case "notes":
			cmd.Flags().String("item", "", "LIST-ENTRY ID from lists items, not media ID (required)")
			_ = cmd.MarkFlagRequired("item")
			cmd.Flags().String("notes", "", "Notes (VIP, maximum 500 characters); explicitly empty clears notes")
			_ = cmd.MarkFlagRequired("notes")
		case "reorder":
			cmd.Flags().StringSlice("rank", nil, "ALL current LIST-ENTRY IDs in desired order, comma-separated (required)")
			_ = cmd.MarkFlagRequired("rank")
		}
		if action != "create" {
			cmd.Flags().String("list", "", "List Trakt ID or slug (required)")
			_ = cmd.MarkFlagRequired("list")
		}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			change := api.ListChange{Action: action}
			change.List, _ = cmd.Flags().GetString("list")
			change.Name, _ = cmd.Flags().GetString("name")
			change.Description, _ = cmd.Flags().GetString("description")
			change.Privacy, _ = cmd.Flags().GetString("privacy")
			change.Type, _ = cmd.Flags().GetString("type")
			change.IDs, _ = cmd.Flags().GetStringSlice("ids")
			change.Item, _ = cmd.Flags().GetString("item")
			change.Rank, _ = cmd.Flags().GetStringSlice("rank")
			if action == "notes" {
				notes, _ := cmd.Flags().GetString("notes")
				change.Notes = &notes
			}
			preview, err := api.PreviewListChange("me", change)
			if err != nil {
				return err
			}
			apply, _ := cmd.Flags().GetBool("apply")
			if !apply {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					api.ListChangePreview
					Note string `json:"note"`
				}{preview, "Offline preview only; me is resolved from credentials only with --apply. Notes require VIP. Reorder must include every list-entry ID; state can change before execution."})
			}
			client := api.NewAPIClient()
			user, err := client.ResolveListOwner()
			if err != nil {
				return fmt.Errorf("resolve authenticated account: %w", err)
			}
			result, applyErr := client.ApplyListChange(user, change)
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
				return err
			}
			return applyErr
		}
		parent.AddCommand(cmd)
	}
}
