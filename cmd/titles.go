package cmd

import (
	"encoding/json"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newTitlesCmd() *cobra.Command {
	parent := &cobra.Command{Use: "titles", Short: "Read title details, seasons and dated episodes without account changes"}
	for _, mode := range []string{"details", "seasons", "episodes", "episode"} {
		mode := mode
		c := &cobra.Command{Use: mode, Args: cobra.NoArgs, SilenceUsage: true}
		c.Flags().String("id", "", "Exact Trakt movie/show media ID (not title/episode ID)")
		_ = c.MarkFlagRequired("id")
		if mode == "details" {
			c.Flags().String("type", "show", "movie or show")
		}
		if mode == "episodes" || mode == "episode" {
			c.Flags().Int("season", 0, "Season number; 0 for specials")
			_ = c.MarkFlagRequired("season")
		}
		if mode == "episode" {
			c.Flags().Int("episode", 0, "Episode number")
			_ = c.MarkFlagRequired("episode")
		}
		c.RunE = func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("id")
			kind := "show"
			if mode == "details" {
				kind, _ = cmd.Flags().GetString("type")
			}
			o := api.TitleBrowseOptions{Mode: mode, Type: kind, ID: id}
			if mode == "episodes" || mode == "episode" {
				n, _ := cmd.Flags().GetInt("season")
				o.Season = &n
			}
			if mode == "episode" {
				n, _ := cmd.Flags().GetInt("episode")
				o.Episode = &n
			}
			if err := api.ValidateTitleBrowse(o); err != nil {
				return err
			}
			client := api.NewAPIClient()
			r, err := client.BrowseTitle(o)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			if !jsonOutput {
				enc.SetIndent("", "  ")
			}
			return enc.Encode(r)
		}
		parent.AddCommand(c)
	}
	return parent
}
func init() { rootCmd.AddCommand(newTitlesCmd()) }
