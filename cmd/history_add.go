package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/briandowns/spinner"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/muesli/termenv"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// historyAddShowResult reports what a single show query would add.
type historyAddShowResult struct {
	Query                  string `json:"query"`
	Matched                string `json:"matched,omitempty"`
	NewEpisodes            int    `json:"new_episodes"`
	AlreadyWatchedEpisodes int    `json:"already_watched_episodes"`
}

// historyAddSkippedShow reports a matched show whose pending episodes could
// not be resolved, so nothing was synced for it.
type historyAddSkippedShow struct {
	Query string `json:"query"`
	Show  string `json:"show"`
	Error string `json:"error"`
}

// filterPendingEpisodes returns the aired episodes that are not in watched,
// grouped as sync seasons, plus how many aired episodes were already
// watched. Episodes without a first_aired date in the past count as unaired
// and are never included.
func filterPendingEpisodes(seasons []api.ShowSeason, watched map[string]bool, now time.Time) ([]api.SyncSeason, int) {
	var pending []api.SyncSeason
	watchedCount := 0
	for _, s := range seasons {
		var eps []api.SyncEpisode
		for _, e := range s.Episodes {
			if e.FirstAired == nil || e.FirstAired.After(now) {
				continue
			}
			if watched[fmt.Sprintf("%d:%d", s.Number, e.Number)] {
				watchedCount++
				continue
			}
			eps = append(eps, api.SyncEpisode{Number: e.Number})
		}
		if len(eps) > 0 {
			pending = append(pending, api.SyncSeason{Number: s.Number, Episodes: eps})
		}
	}
	return pending, watchedCount
}

// pendingEpisodesForShow wraps filterPendingEpisodes with the show's seasons
// and the user's watched episode set.
func pendingEpisodesForShow(client *api.APIClient, showID int) ([]api.SyncSeason, int, error) {
	watched, err := client.WatchedEpisodeSet(showID)
	if err != nil {
		return nil, 0, err
	}
	seasons, err := client.GetShowSeasons(showID)
	if err != nil {
		return nil, 0, err
	}
	pending, watchedCount := filterPendingEpisodes(seasons, watched, time.Now())
	return pending, watchedCount, nil
}

var historyAddCmd = &cobra.Command{
	Use:   "add [show or movie names...]",
	Short: "Add items to your watch history",
	Long:  `Search for shows or movies by name and add them to your watch history.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()
		itemType, _ := cmd.Flags().GetString("type")
		watchedAt, _ := cmd.Flags().GetString("watched-at")

		if itemType == "" {
			itemType = "show"
		}

		// Validate watched-at if provided
		if watchedAt != "" {
			if _, err := time.Parse(time.RFC3339, watchedAt); err != nil {
				// Try date-only format and convert to RFC3339
				if t, err2 := time.Parse("2006-01-02", watchedAt); err2 == nil {
					watchedAt = t.UTC().Format(time.RFC3339)
				} else {
					logrus.Fatalf("Invalid --watched-at format. Use RFC3339 (2023-01-15T00:00:00Z) or date (2023-01-15)")
				}
			}
		}

		searchType := itemType
		if searchType == "movie" {
			searchType = "movie"
		} else {
			searchType = "show"
		}

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)

		syncReq := &api.SyncHistoryReq{}
		var showResults []historyAddShowResult
		var skippedShows []historyAddSkippedShow
		matchedAny := false

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{
			termenv.String("Query").Bold(),
			termenv.String("Matched").Bold(),
			termenv.String("Year").Bold(),
			termenv.String("Trakt ID").Bold(),
		})

		for _, query := range args {
			if !jsonOutput {
				s.Prefix = fmt.Sprintf("Searching for \"%s\"... ", query)
				s.Start()
			}

			results, err := client.Search(query, searchType)
			s.Stop()
			if err != nil {
				logrus.WithError(err).Errorf("Failed to search for %s", query)
				continue
			}

			if len(results) == 0 {
				p := termenv.ColorProfile()
				t.AppendRow([]interface{}{
					query,
					termenv.String("NOT FOUND").Foreground(p.Color("#FF6B6B")),
					"",
					"",
				})
				continue
			}

			// Prefer exact title match over first result
			result := results[0]
			queryLower := strings.ToLower(strings.TrimSpace(query))
			for _, r := range results {
				var title string
				if searchType == "movie" && r.Movie != nil {
					title = r.Movie.Title
				} else if r.Show != nil {
					title = r.Show.Title
				}
				if strings.ToLower(title) == queryLower {
					result = r
					break
				}
			}
			item := api.SyncItem{}
			item.WatchedAt = watchedAt

			if searchType == "movie" && result.Movie != nil {
				item.Ids.Trakt = result.Movie.Ids.Trakt
				syncReq.Movies = append(syncReq.Movies, item)
				matchedAny = true
				t.AppendRow([]interface{}{
					query,
					result.Movie.Title,
					result.Movie.Year,
					result.Movie.Ids.Trakt,
				})
			} else if result.Show != nil {
				// Narrow the sync to aired episodes the user has not
				// watched yet: a bare show item makes Trakt add a new play
				// for every aired episode, including ones already watched.
				pending, watchedCount, err := pendingEpisodesForShow(&client, result.Show.Ids.Trakt)
				if err != nil {
					logrus.WithError(err).Warnf("Skipping %s: could not resolve pending episodes", result.Show.Title)
					p := termenv.ColorProfile()
					skippedShows = append(skippedShows, historyAddSkippedShow{
						Query: query,
						Show:  result.Show.Title,
						Error: err.Error(),
					})
					t.AppendRow([]interface{}{
						query,
						result.Show.Title,
						result.Show.Year,
						termenv.String("SKIPPED").Foreground(p.Color("#FF6B6B")),
					})
					continue
				}
				res := historyAddShowResult{
					Query:                  query,
					Matched:                result.Show.Title,
					AlreadyWatchedEpisodes: watchedCount,
				}
				for _, s := range pending {
					res.NewEpisodes += len(s.Episodes)
				}
				if len(pending) == 0 {
					matchedAny = true
					t.AppendRow([]interface{}{
						query,
						result.Show.Title,
						result.Show.Year,
						fmt.Sprintf("already watched (%d eps)", watchedCount),
					})
					showResults = append(showResults, res)
					continue
				}
				item.Ids.Trakt = result.Show.Ids.Trakt
				item.Seasons = pending
				syncReq.Shows = append(syncReq.Shows, item)
				matchedAny = true
				showResults = append(showResults, res)
				t.AppendRow([]interface{}{
					query,
					result.Show.Title,
					result.Show.Year,
					result.Show.Ids.Trakt,
				})
			}
		}

		if !jsonOutput {
			t.SetStyle(table.StyleRounded)
			t.Render()
		}

		if len(syncReq.Shows) == 0 && len(syncReq.Movies) == 0 {
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if len(skippedShows) > 0 {
					_ = enc.Encode(map[string]interface{}{
						"error":         "pending episode lookup failed",
						"skipped_shows": skippedShows,
					})
					os.Exit(1)
				}
				if matchedAny {
					_ = enc.Encode(map[string]interface{}{
						"added_movies":     0,
						"added_episodes":   0,
						"not_found_movies": 0,
						"not_found_shows":  0,
						"shows":            showResults,
					})
				} else {
					fmt.Println("{\"error\": \"no items matched\"}")
				}
				return
			}
			if len(skippedShows) > 0 {
				fmt.Println("\nFailed to resolve pending episodes; nothing added.")
				os.Exit(1)
			}
			if matchedAny {
				fmt.Println("\nNo new episodes to add.")
			} else {
				fmt.Println("\nNo items to add.")
			}
			return
		}

		if !jsonOutput {
			fmt.Printf("\nAdding %d shows and %d movies to history...\n", len(syncReq.Shows), len(syncReq.Movies))
		}

		if !jsonOutput {
			s.Prefix = "Syncing... "
			s.Start()
		}
		resp, err := client.SyncHistory(syncReq)
		s.Stop()
		if err != nil {
			logrus.WithError(err).Fatal("Failed to sync history")
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			payload := map[string]interface{}{
				"added_movies":     resp.Added.Movies,
				"added_episodes":   resp.Added.Episodes,
				"not_found_movies": len(resp.NotFound.Movies),
				"not_found_shows":  len(resp.NotFound.Shows),
				"shows":            showResults,
			}
			if len(skippedShows) > 0 {
				payload["skipped_shows"] = skippedShows
			}
			_ = enc.Encode(payload)
			return
		}

		fmt.Printf("Added: %d movies, %d episodes\n", resp.Added.Movies, resp.Added.Episodes)
		if len(resp.NotFound.Movies) > 0 || len(resp.NotFound.Shows) > 0 {
			fmt.Printf("Not found: %d movies, %d shows\n", len(resp.NotFound.Movies), len(resp.NotFound.Shows))
		}
		if len(skippedShows) > 0 {
			fmt.Printf("Skipped (could not resolve pending episodes): %d shows\n", len(skippedShows))
		}
	},
}

func init() {
	historyCmd.AddCommand(historyAddCmd)

	historyAddCmd.Flags().String("type", "show", "Type of item (show, movie)")
	historyAddCmd.Flags().String("watched-at", "", "When the items were watched (RFC3339 or YYYY-MM-DD). Defaults to now")
}
