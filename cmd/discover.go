package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newDiscoverCmd() *cobra.Command {
	c := &cobra.Command{Use: "discover <trending|popular|recommendations|related>", Short: "Discover ranked movies/shows and related titles without changing your account", Args: cobra.ExactArgs(1), SilenceUsage: true}
	c.Flags().String("type", "show", "movie or show")
	c.Flags().String("id", "", "Exact Trakt media ID; required only for related")
	c.Flags().Int("page", 1, "Page number (recommendations supports page 1 only)")
	c.Flags().Int("limit", 10, "Maximum requested results (1-100)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		kind, _ := cmd.Flags().GetString("type")
		id, _ := cmd.Flags().GetString("id")
		page, _ := cmd.Flags().GetInt("page")
		limit, _ := cmd.Flags().GetInt("limit")
		o := api.DiscoveryOptions{Mode: args[0], Type: kind, ID: id, Page: page, Limit: limit}
		if err := api.ValidateDiscovery(o); err != nil {
			return err
		}
		client := api.NewAPIClient()
		r, err := client.Discover(o)
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
		}
		for _, item := range r.Items {
			year := "unknown"
			if item.Year != nil {
				year = fmt.Sprint(*item.Year)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) [%s %s]\n", item.Title, year, item.Type, item.TraktID); err != nil {
				return err
			}
		}
		if len(r.Items) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "No discovery results returned.")
		}
		return err
	}
	return c
}
func init() { rootCmd.AddCommand(newDiscoverCmd()) }
