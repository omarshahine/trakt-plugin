package cmd

import (
	"encoding/json"
	"errors"
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

// historyAddQueryResult reports what a single requested query resolved to,
// for shows and movies alike. Every requested query gets an entry so JSON
// callers can account for all titles; shows whose pending-episode lookup
// failed are reported via skipped_shows instead.
type historyAddQueryResult struct {
	Query string `json:"query"`
	Type  string `json:"type"` // "show" or "movie"
	// Matched is omitted when the query resolved to nothing; SearchError
	// distinguishes a failed search from a genuine no-result search.
	Matched string `json:"matched,omitempty"`
	// Episode counters are always present on show results (zero included)
	// and omitted for movies, where they carry no meaning.
	NewEpisodes            *int   `json:"new_episodes,omitempty"`
	AlreadyWatchedEpisodes *int   `json:"already_watched_episodes,omitempty"`
	// DuplicateOf is set to the earlier query that already handled this
	// show when several arguments resolve to the same Trakt ID. The entry
	// adds nothing itself; it exists so every requested query gets a
	// result in JSON mode.
	DuplicateOf string `json:"duplicate_of,omitempty"`
	// SearchError is set instead of Matched when the Trakt search itself
	// failed for this query.
	SearchError string `json:"search_error,omitempty"`
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

// abortOnRateLimit ends the command as soon as the Trakt API rate-limits a
// search or a pending-episode lookup: processing further items would only
// hit the same bucket, and a later successful sync would hide the failure
// from wrappers that need to back off. Nothing is synced on this path, so a
// retried invocation after backoff cannot create duplicate plays.
func abortOnRateLimit(err error) {
	var rlErr *api.RateLimitError
	if !errors.As(err, &rlErr) {
		return
	}
	// The marker line must also reach stderr: wrappers like the bundled
	// OpenClaw tool build their error output from message + stderr on a
	// nonzero exit and match the RateLimitError format to start backing
	// off — stdout-only markers never reach that detector.
	fmt.Fprintln(os.Stderr, rlErr.Error())
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		payload := map[string]interface{}{"error": rlErr.Error()}
		if rlErr.RetryAfterSeconds > 0 {
			payload["retry_after"] = rlErr.RetryAfterSeconds
		}
		_ = enc.Encode(payload)
	} else {
		fmt.Println("\nRate limited by the Trakt API; nothing added.")
		if rlErr.RetryAfterSeconds > 0 {
			fmt.Printf("Retry after %d seconds.\n", rlErr.RetryAfterSeconds)
		}
	}
	os.Exit(1)
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
		var queryResults []historyAddQueryResult
		var skippedShows []historyAddSkippedShow
		// Two arguments can resolve to the same show (repeated title or an
		// alias); each lookup would see the same pre-sync history and queue
		// the same pending episodes, so the sync would create two plays per
		// episode. Handle every show at most once per invocation.
		queuedShows := make(map[int]string)
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
				abortOnRateLimit(err)
				logrus.WithError(err).Errorf("Failed to search for %s", query)
				p := termenv.ColorProfile()
				t.AppendRow([]interface{}{
					query,
					termenv.String("SEARCH FAILED").Foreground(p.Color("#FF6B6B")),
					"",
					"",
				})
				queryResults = append(queryResults, historyAddQueryResult{
					Query:       query,
					Type:        searchType,
					SearchError: err.Error(),
				})
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
				queryResults = append(queryResults, historyAddQueryResult{Query: query, Type: searchType})
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
				queryResults = append(queryResults, historyAddQueryResult{
					Query:   query,
					Type:    searchType,
					Matched: result.Movie.Title,
				})
			} else if result.Show != nil {
				if firstQuery, dup := queuedShows[result.Show.Ids.Trakt]; dup {
					p := termenv.ColorProfile()
					t.AppendRow([]interface{}{
						query,
						result.Show.Title,
						result.Show.Year,
						termenv.String(fmt.Sprintf("duplicate of %q", firstQuery)).Foreground(p.Color("#FF6B6B")),
					})
					queryResults = append(queryResults, historyAddQueryResult{
						Query:       query,
						Type:        searchType,
						Matched:     result.Show.Title,
						DuplicateOf: firstQuery,
					})
					continue
				}
				// Narrow the sync to aired episodes the user has not
				// watched yet: a bare show item makes Trakt add a new play
				// for every aired episode, including ones already watched.
				pending, watchedCount, err := pendingEpisodesForShow(&client, result.Show.Ids.Trakt)
				if err != nil {
					abortOnRateLimit(err)
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
				queuedShows[result.Show.Ids.Trakt] = query
				newEpisodes := 0
				for _, s := range pending {
					newEpisodes += len(s.Episodes)
				}
				watched := watchedCount
				res := historyAddQueryResult{
					Query:                  query,
					Type:                   searchType,
					Matched:                result.Show.Title,
					NewEpisodes:            &newEpisodes,
					AlreadyWatchedEpisodes: &watched,
				}
				if len(pending) == 0 {
					matchedAny = true
					t.AppendRow([]interface{}{
						query,
						result.Show.Title,
						result.Show.Year,
						fmt.Sprintf("already watched (%d eps)", watchedCount),
					})
					queryResults = append(queryResults, res)
					continue
				}
				item.Ids.Trakt = result.Show.Ids.Trakt
				item.Seasons = pending
				syncReq.Shows = append(syncReq.Shows, item)
				matchedAny = true
				queryResults = append(queryResults, res)
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
						"queries":       queryResults,
					})
					os.Exit(1)
				}
				if matchedAny {
					_ = enc.Encode(map[string]interface{}{
						"added_movies":       0,
						"added_episodes":     0,
						"not_found_movies":   0,
						"not_found_shows":    0,
						"not_found_seasons":  0,
						"not_found_episodes": 0,
						"queries":            queryResults,
					})
				} else {
					// Every requested query gets an entry in queries, so a
					// stdout-only consumer can tell a failed search
					// (search_error) apart from a genuine no-match.
					_ = enc.Encode(map[string]interface{}{
						"error":   "no items matched",
						"queries": queryResults,
					})
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
				"added_movies":       resp.Added.Movies,
				"added_episodes":     resp.Added.Episodes,
				"not_found_movies":   len(resp.NotFound.Movies),
				"not_found_shows":    len(resp.NotFound.Shows),
				"not_found_seasons":  len(resp.NotFound.Seasons),
				"not_found_episodes": len(resp.NotFound.Episodes),
				"queries":            queryResults,
			}
			if len(skippedShows) > 0 {
				payload["skipped_shows"] = skippedShows
			}
			_ = enc.Encode(payload)
			return
		}

		fmt.Printf("Added: %d movies, %d episodes\n", resp.Added.Movies, resp.Added.Episodes)
		if len(resp.NotFound.Movies) > 0 || len(resp.NotFound.Shows) > 0 ||
			len(resp.NotFound.Seasons) > 0 || len(resp.NotFound.Episodes) > 0 {
			fmt.Printf("Not found: %d movies, %d shows, %d seasons, %d episodes\n",
				len(resp.NotFound.Movies), len(resp.NotFound.Shows),
				len(resp.NotFound.Seasons), len(resp.NotFound.Episodes))
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
