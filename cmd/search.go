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

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for movies and TV shows",
	Long:  `Search for movies and TV shows on Trakt.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()

		query := strings.Join(args, " ")

		searchType, err := cmd.Flags().GetString("type")
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get type flag")
		}

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)
		if !jsonOutput {
			s.Start()
			s.Prefix = fmt.Sprintf("Searching for '%s'... ", query)
		}

		results, err := client.Search(query, searchType)
		if err != nil {
			s.Stop()
			logrus.WithError(err).Fatal("Search failed")
		}

		s.Stop()

		if len(results) == 0 {
			if jsonOutput {
				fmt.Println("{\"items\": []}")
			} else {
				fmt.Println("No results found.")
			}
			return
		}

		if jsonOutput {
			type jsonSearchItem struct {
				Type    string `json:"type"`
				Title   string `json:"title"`
				Year    int    `json:"year"`
				TraktID int    `json:"trakt_id"`
				IMDB    string `json:"imdb"`
				Score   float64 `json:"score"`
			}
			var items []jsonSearchItem
			for _, r := range results {
				switch r.Type {
				case "movie":
					if r.Movie != nil {
						items = append(items, jsonSearchItem{Type: "movie", Title: r.Movie.Title, Year: r.Movie.Year, TraktID: r.Movie.Ids.Trakt, IMDB: r.Movie.Ids.Imdb, Score: r.Score})
					}
				case "show":
					if r.Show != nil {
						items = append(items, jsonSearchItem{Type: "show", Title: r.Show.Title, Year: r.Show.Year, TraktID: r.Show.Ids.Trakt, IMDB: r.Show.Ids.Imdb, Score: r.Score})
					}
				}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"items": items})
			return
		}

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{
			termenv.String("Type").Bold(),
			termenv.String("Title").Bold(),
			termenv.String("Year").Bold(),
			termenv.String("IMDB").Bold(),
		})

		for _, r := range results {
			switch r.Type {
			case "movie":
				if r.Movie != nil {
					t.AppendRow([]interface{}{
						"Movie",
						r.Movie.Title,
						r.Movie.Year,
						r.Movie.Ids.Imdb,
					})
				}
			case "show":
				if r.Show != nil {
					t.AppendRow([]interface{}{
						"TV Show",
						r.Show.Title,
						r.Show.Year,
						r.Show.Ids.Imdb,
					})
				}
			}
		}

		t.SetStyle(table.StyleRounded)
		t.Render()
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().StringP("type", "t", "movie,show", "Type to search for (movie, show, or movie,show)")
}
