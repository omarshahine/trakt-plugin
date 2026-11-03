package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newPreferencesCmd(resource string) *cobra.Command {
	parent := &cobra.Command{Use: resource, Short: "Read or preview-first manage your " + resource, Args: cobra.NoArgs, SilenceUsage: true}
	parent.PersistentFlags().String("type", "all", "Media type: movie, show, all; ratings also supports season and episode")
	parent.Flags().Int("page", 1, "Page number")
	parent.Flags().Int("limit", 10, "Records per page (1-1000)")
	parent.RunE = func(cmd *cobra.Command, _ []string) error {
		kind, _ := cmd.Flags().GetString("type")
		page, _ := cmd.Flags().GetInt("page")
		limit, _ := cmd.Flags().GetInt("limit")
		if !validPreferenceType(resource, kind, true) || page < 1 || limit < 1 || limit > 1000 {
			return fmt.Errorf("invalid type/page/limit for %s", resource)
		}
		client := api.NewAPIClient()
		user, err := client.ResolveListOwner()
		if err != nil {
			return err
		}
		rows, pagination, err := client.GetPreferences(user, resource, kind, api.PaginationsParams{Page: page, Limit: limit})
		if err != nil {
			return err
		}
		return writeCustomLists(cmd.OutOrStdout(), rows, pagination, jsonOutput)
	}
	addAction := "set"
	if resource == "favorites" {
		addAction = "add"
	}
	for _, action := range []string{addAction, "remove"} {
		action := action
		child := &cobra.Command{Use: action, Short: "Preview " + resource + " " + action + "; --apply explicitly writes", Args: cobra.NoArgs, SilenceUsage: true}
		child.Flags().StringSlice("ids", nil, "Trakt MEDIA IDs, comma-separated (required)")
		_ = child.MarkFlagRequired("ids")
		child.Flags().Bool("apply", false, "Execute the write; otherwise an offline preview")
		child.Flags().String("expect-user", "", "Authenticated account slug required with --apply; mismatch refuses the write")
		if resource == "ratings" && action == "set" {
			child.Flags().Int("rating", 0, "Integer 1-10 (required)")
			_ = child.MarkFlagRequired("rating")
			child.Flags().String("rated-at", "", "Optional ISO8601 timestamp; normalized to UTC, omitted defaults server-side to now")
		}
		child.RunE = func(cmd *cobra.Command, _ []string) error {
			kind, _ := cmd.Flags().GetString("type")
			ids, _ := cmd.Flags().GetStringSlice("ids")
			rating, _ := cmd.Flags().GetInt("rating")
			ratedAt, _ := cmd.Flags().GetString("rated-at")
			change := api.PreferenceChange{Resource: resource, Action: action, Type: kind, IDs: ids, Rating: rating, RatedAt: ratedAt}
			preview, err := api.PreviewPreferenceChange(change)
			if err != nil {
				return err
			}
			apply, _ := cmd.Flags().GetBool("apply")
			expected, _ := cmd.Flags().GetString("expect-user")
			if !apply {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Resource string `json:"resource"`
					api.ListChangePreview
					Expected string `json:"expected_account,omitempty"`
					Note     string `json:"note"`
				}{resource, preview, expected, "Offline preview, no login or network. Apply requires expect-user to match the freshly authenticated account. Setting a rating can replace its existing value."})
			}
			if expected == "" {
				return fmt.Errorf("expect-user is required with apply")
			}
			client := api.NewAPIClient()
			result, applyErr := client.ApplyPreferenceChange(expected, change)
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
				return err
			}
			return applyErr
		}
		parent.AddCommand(child)
	}
	return parent
}

func validPreferenceType(resource, kind string, all bool) bool {
	return kind == "movie" || kind == "show" || (all && kind == "all") || (resource == "ratings" && (kind == "season" || kind == "episode"))
}

func init() { rootCmd.AddCommand(newPreferencesCmd("ratings"), newPreferencesCmd("favorites")) }
