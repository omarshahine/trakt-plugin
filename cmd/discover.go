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
	c.Flags().Bool("exclude-watched", false, "Exclude any history play; one episode excludes its show")
	c.Flags().Bool("exclude-watchlisted", false, "Exclude watchlisted movies/shows, including parent shows of episode/season entries")
	c.Flags().StringSlice("exclude-ids", nil, "Previously seen IDs for this media type; comma-separated")
	c.Flags().String("shortlist", "", "Prepare an offline list-add preview for remaining results; never applies")
	c.Flags().StringSlice("genres", nil, "Match any selected Trakt genre slug; inspect with genres command")
	for _, name := range []string{"year-min", "year-max", "runtime-min", "runtime-max"} {
		c.Flags().Int(name, 0, "Inclusive page-local metadata bound; runtime is minutes")
	}
	c.Flags().Float64("min-rating", 0, "Minimum known community score (0-10); unrated excluded")
	c.Flags().Int64("min-votes", 0, "Minimum community vote count")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		kind, _ := cmd.Flags().GetString("type")
		id, _ := cmd.Flags().GetString("id")
		page, _ := cmd.Flags().GetInt("page")
		limit, _ := cmd.Flags().GetInt("limit")
		watched, _ := cmd.Flags().GetBool("exclude-watched")
		watchlisted, _ := cmd.Flags().GetBool("exclude-watchlisted")
		ids, _ := cmd.Flags().GetStringSlice("exclude-ids")
		shortlist, _ := cmd.Flags().GetString("shortlist")
		o := api.DiscoveryOptions{ExcludeWatched: watched, ExcludeWatchlisted: watchlisted, ExcludeIDs: ids, Shortlist: shortlist, Mode: args[0], Type: kind, ID: id, Page: page, Limit: limit}
		o.Criteria.Genres, _ = cmd.Flags().GetStringSlice("genres")
		for name, target := range map[string]**int{"year-min": &o.Criteria.YearMin, "year-max": &o.Criteria.YearMax, "runtime-min": &o.Criteria.RuntimeMin, "runtime-max": &o.Criteria.RuntimeMax} {
			if cmd.Flags().Changed(name) {
				n, _ := cmd.Flags().GetInt(name)
				*target = &n
			}
		}
		if cmd.Flags().Changed("min-rating") {
			n, _ := cmd.Flags().GetFloat64("min-rating")
			o.Criteria.MinRating = &n
		}
		if cmd.Flags().Changed("min-votes") {
			n, _ := cmd.Flags().GetInt64("min-votes")
			o.Criteria.MinVotes = &n
		}
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
		if r.Shortlist != nil {
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(r.Shortlist); err != nil {
				return err
			}
		}
		if len(r.Items) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "No titles returned after this response’s filters; other pages may differ.")
		}
		return err
	}
	return c
}
func init() { rootCmd.AddCommand(newDiscoverCmd()) }
