package cmd

import (
	"encoding/json"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newEpisodeQueueCmd() *cobra.Command {
	c := &cobra.Command{Use: "episode-tonight", Short: "Suggest next aired unwatched episodes from started shows within a bounded show window", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().Int("count", 5, "Requested episode suggestions (1-20), one per show")
	c.Flags().Int("max-shows", 5, "Maximum progress reads for recent-history shows (1-20)")
	c.Flags().Int("runtime-max", 0, "Optional per-episode maximum minutes; missing runtime excluded")
	c.Flags().Bool("include-specials", false, "Allow season-zero history and progress candidates; no custom special ordering")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		o := api.EpisodeQueueOptions{}
		o.Count, _ = cmd.Flags().GetInt("count")
		o.MaxShows, _ = cmd.Flags().GetInt("max-shows")
		o.IncludeSpecials, _ = cmd.Flags().GetBool("include-specials")
		if cmd.Flags().Changed("runtime-max") {
			n, _ := cmd.Flags().GetInt("runtime-max")
			o.RuntimeMax = &n
		}
		if err := api.ValidateEpisodeQueue(o); err != nil {
			return err
		}
		client := api.NewAPIClient()
		r, scanErr := client.BuildEpisodeQueue(o)
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
func init() { rootCmd.AddCommand(newEpisodeQueueCmd()) }
