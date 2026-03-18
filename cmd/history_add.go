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
			_ = enc.Encode(map[string]interface{}{
				"added_movies":   resp.Added.Movies,
				"added_episodes": resp.Added.Episodes,
				"not_found_movies": len(resp.NotFound.Movies),
				"not_found_shows":  len(resp.NotFound.Shows),
			})
			return
		}

		fmt.Printf("Added: %d movies, %d episodes\n", resp.Added.Movies, resp.Added.Episodes)
		if len(resp.NotFound.Movies) > 0 || len(resp.NotFound.Shows) > 0 {
			fmt.Printf("Not found: %d movies, %d shows\n", len(resp.NotFound.Movies), len(resp.NotFound.Shows))
		}
	},
}

func init() {
	historyCmd.AddCommand(historyAddCmd)

	historyAddCmd.Flags().String("type", "show", "Type of item (show, movie)")
	historyAddCmd.Flags().String("watched-at", "", "When the items were watched (RFC3339 or YYYY-MM-DD). Defaults to now")
}
