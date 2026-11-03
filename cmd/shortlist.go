package cmd

import (
	"encoding/json"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newShortlistCmd() *cobra.Command {
	c := &cobra.Command{Use: "shortlist", Short: "Build a bounded multi-page watch-tonight shortlist with factual criteria evidence", Args: cobra.NoArgs, SilenceUsage: true}
	addDiscoveryFlags(c, true, 20)
	c.Flags().String("mode", "popular", "trending, popular, related or recommendations")
	c.Flags().Int("count", 5, "Desired matches (1-50)")
	c.Flags().Int("max-pages", 5, "Maximum catalog requests (1-20); recommendations reads once")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		mode, _ := cmd.Flags().GetString("mode")
		o, err := readDiscoveryOptions(cmd, mode)
		if err != nil {
			return err
		}
		count, _ := cmd.Flags().GetInt("count")
		pages, _ := cmd.Flags().GetInt("max-pages")
		opts := api.ShortlistOptions{DiscoveryOptions: o, Count: count, MaxPages: pages}
		if err := api.ValidateShortlist(opts); err != nil {
			return err
		}
		client := api.NewAPIClient()
		r, scanErr := client.BuildShortlist(opts)
		enc := json.NewEncoder(cmd.OutOrStdout())
		if !jsonOutput {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
		return scanErr
	}
	return c
}
func init() { rootCmd.AddCommand(newShortlistCmd()) }
