package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/muesli/termenv"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// watchlistAddCmd mirrors history_add: search each query against Trakt,
// pick the best match by title, then POST all matches in one /sync/watchlist
// call. Items already on the user's list are reported under `existing` by
// the server and surfaced as a non-fatal count in the output.
var watchlistAddCmd = &cobra.Command{
	Use:   "add [show or movie names...]",
	Short: "Add items to your watchlist",
	Long:  `Search for shows or movies by name and add them to your Trakt watchlist.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()
		// itemType is a cobra flag with default "show" — no empty-string
		// fallback needed. We still narrow anything that isn't literally
		// "movie" down to "show" so the /search/{type} path stays valid.
		itemType, _ := cmd.Flags().GetString("type")
		searchType := itemType
		if searchType != "movie" {
			searchType = "show"
		}

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)

		syncReq := &api.SyncWatchlistReq{}

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

			if searchType == "movie" && result.Movie != nil {
				item.Ids.Trakt = result.Movie.Ids.Trakt
				syncReq.Movies = append(syncReq.Movies, item)
				t.AppendRow([]interface{}{
					query,
					result.Movie.Title,
					result.Movie.Year,
					result.Movie.Ids.Trakt,
				})
			} else if result.Show != nil {
				item.Ids.Trakt = result.Show.Ids.Trakt
				syncReq.Shows = append(syncReq.Shows, item)
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
				fmt.Println("{\"error\": \"no items matched\"}")
			} else {
				fmt.Println("\nNo items to add.")
			}
			return
		}

		if !jsonOutput {
			fmt.Printf("\nAdding %d shows and %d movies to watchlist...\n", len(syncReq.Shows), len(syncReq.Movies))
			s.Prefix = "Syncing... "
			s.Start()
		}
		resp, err := client.AddWatchlist(syncReq)
		s.Stop()
		if err != nil {
			logrus.WithError(err).Fatal("Failed to add to watchlist")
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]interface{}{
				"added_movies":     resp.Added.Movies,
				"added_shows":      resp.Added.Shows,
				"existing_movies":  resp.Existing.Movies,
				"existing_shows":   resp.Existing.Shows,
				"not_found_movies": len(resp.NotFound.Movies),
				"not_found_shows":  len(resp.NotFound.Shows),
			})
			return
		}

		fmt.Printf("Added: %d movies, %d shows\n", resp.Added.Movies, resp.Added.Shows)
		if resp.Existing.Movies > 0 || resp.Existing.Shows > 0 {
			fmt.Printf("Already on watchlist: %d movies, %d shows\n", resp.Existing.Movies, resp.Existing.Shows)
		}
		if len(resp.NotFound.Movies) > 0 || len(resp.NotFound.Shows) > 0 {
			fmt.Printf("Not found: %d movies, %d shows\n", len(resp.NotFound.Movies), len(resp.NotFound.Shows))
		}
	},
}

func init() {
	watchlistCmd.AddCommand(watchlistAddCmd)

	watchlistAddCmd.Flags().String("type", "show", "Type of item (show, movie)")
}
