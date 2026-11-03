package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

// newListsCmd keeps command state local so tests can execute fresh commands.
func newListsCmd() *cobra.Command {
	lists := &cobra.Command{
		Use: "lists", Short: "View your custom lists",
		Long: "Read your custom lists, including private lists accessible to your authenticated account. This does not modify lists.",
		Args: cobra.NoArgs,
		RunE: runCustomLists(false),
	}
	lists.PersistentFlags().Int("page", 1, "Page number (positive)")
	lists.PersistentFlags().Int("limit", 10, "Items per page (1-1000)")
	items := &cobra.Command{
		Use: "items", Short: "View the contents of a custom list",
		Args: cobra.NoArgs,
		RunE: runCustomLists(true),
	}
	items.Flags().String("list", "", "Custom list Trakt ID or slug (required)")
	_ = items.MarkFlagRequired("list")
	lists.AddCommand(items)
	return lists
}

func runCustomLists(items bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		page, _ := cmd.Flags().GetInt("page")
		limit, _ := cmd.Flags().GetInt("limit")
		if page < 1 || limit < 1 || limit > 1000 {
			return fmt.Errorf("page must be positive and limit must be between 1 and 1000")
		}
		list, _ := cmd.Flags().GetString("list")
		if items && list == "" {
			return fmt.Errorf("--list must not be empty")
		}
		client := api.NewAPIClient()
		slug, err := client.GetUserSlug()
		if err != nil {
			return fmt.Errorf("resolve trakt user: %w", err)
		}
		params := api.PaginationsParams{Page: page, Limit: limit}
		var records []json.RawMessage
		var pagination api.Pagination
		if items {
			records, pagination, err = client.GetUserListItems(slug, list, params)
		} else {
			records, pagination, err = client.GetUserLists(slug, params)
		}
		if err != nil {
			return err
		}
		return writeCustomLists(cmd.OutOrStdout(), records, pagination, jsonOutput)
	}
}

func writeCustomLists(out io.Writer, records []json.RawMessage, pagination api.Pagination, asJSON bool) error {
	if records == nil {
		records = []json.RawMessage{}
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Items []json.RawMessage `json:"items"`
			api.Pagination
		}{records, pagination})
	}
	// Pretty-print raw records for human use too, retaining mixed item types
	// and metadata instead of silently dropping fields we do not recognize.
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out, "Page %s out of %s, %s items in total\n", pagination.Page, pagination.PageCount, pagination.ItemCount)
	return err
}

func init() { rootCmd.AddCommand(newListsCmd()) }
