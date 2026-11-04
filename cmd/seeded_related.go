package cmd

import (
	"encoding/json"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newSeededRelatedCmd() *cobra.Command {
	c := &cobra.Command{Use: "seeded-related", Short: "Bounded related-title recommendations seeded by account favorites and ratings", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().String("type", "show", "movie or show")
	c.Flags().String("source", "both", "both, favorites or ratings")
	c.Flags().Int("min-seed-rating", 8, "Minimum personal rating for a rating seed (1-10); favorites qualify independently")
	c.Flags().Int("max-seeds", 3, "Maximum seed-related requests (1-10)")
	c.Flags().Int("per-seed", 10, "First-page related results per seed (1-20)")
	c.Flags().Int("count", 5, "Maximum returned unique suggestions (1-50)")
	c.Flags().Bool("exclude-watchlisted", true, "Exclude watchlisted titles in addition to mandatory watched exclusion")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		o := api.SeededRelatedOptions{}
		o.Type, _ = cmd.Flags().GetString("type")
		o.Source, _ = cmd.Flags().GetString("source")
		o.MinSeedRating, _ = cmd.Flags().GetInt("min-seed-rating")
		o.MaxSeeds, _ = cmd.Flags().GetInt("max-seeds")
		o.PerSeed, _ = cmd.Flags().GetInt("per-seed")
		o.Count, _ = cmd.Flags().GetInt("count")
		o.ExcludeWatchlisted, _ = cmd.Flags().GetBool("exclude-watchlisted")
		if err := api.ValidateSeededRelated(o); err != nil {
			return err
		}
		client := api.NewAPIClient()
		r, scanErr := client.BuildSeededRelated(o)
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
func init() { rootCmd.AddCommand(newSeededRelatedCmd()) }
