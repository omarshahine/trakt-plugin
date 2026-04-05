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

// historyRemoveCmd is the symmetric inverse of history_add. Removing by
// trakt id wipes ALL plays of that item from history — there's no partial
// "remove one watch on this date" semantics, which is why there's no
// --watched-at flag on this command.
var historyRemoveCmd = &cobra.Command{
	Use:   "remove [show or movie names...]",
	Short: "Remove items from your watch history",
	Long:  `Search for shows or movies by name and remove them from your Trakt watch history. Removing a show deletes all episode plays; removing a movie deletes all watches.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()
		itemType, _ := cmd.Flags().GetString("type")
		searchType := itemType
		if searchType != "movie" {
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
				fmt.Println("\nNo items to remove.")
			}
			return
		}

		if !jsonOutput {
			fmt.Printf("\nRemoving %d shows and %d movies from history...\n", len(syncReq.Shows), len(syncReq.Movies))
			s.Prefix = "Syncing... "
			s.Start()
		}
		resp, err := client.RemoveHistory(syncReq)
		s.Stop()
		if err != nil {
			logrus.WithError(err).Fatal("Failed to remove from history")
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]interface{}{
				"deleted_movies":   resp.Deleted.Movies,
				"deleted_episodes": resp.Deleted.Episodes,
				"not_found_movies": len(resp.NotFound.Movies),
				"not_found_shows":  len(resp.NotFound.Shows),
			})
			return
		}

		fmt.Printf("Deleted: %d movies, %d episodes\n", resp.Deleted.Movies, resp.Deleted.Episodes)
		if len(resp.NotFound.Movies) > 0 || len(resp.NotFound.Shows) > 0 {
			fmt.Printf("Not found: %d movies, %d shows\n", len(resp.NotFound.Movies), len(resp.NotFound.Shows))
		}
	},
}

func init() {
	historyCmd.AddCommand(historyRemoveCmd)
	historyRemoveCmd.Flags().String("type", "show", "Type of item (show, movie)")
}
