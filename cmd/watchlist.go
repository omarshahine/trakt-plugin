package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/briandowns/spinner"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/mergestat/timediff"
	"github.com/muesli/termenv"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var watchlistCmd = &cobra.Command{
	Use:   "watchlist",
	Short: "Show your watchlist",
	Long:  `Show your watchlist of movies and TV shows you want to watch.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)
		if !jsonOutput {
			s.Start()
			s.Prefix = "Loading watchlist... "
		}

		settings, err := client.GetUserSettings()
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get user settings")
		}

		page, err := cmd.Flags().GetInt("page")
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get page")
		}
		limit, err := cmd.Flags().GetInt("limit")
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get limit")
		}
		listType, err := cmd.Flags().GetString("type")
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get type")
		}

		resp, pagination, err := client.GetUserWatchlist(settings.User.Ids.Slug, listType, api.PaginationsParams{
			Page:  page,
			Limit: limit,
		})
		if err != nil {
			fmt.Println(err)
			return
		}

		s.Stop()

		if jsonOutput {
			type jsonItem struct {
				Type    string `json:"type"`
				Title   string `json:"title"`
				Year    int    `json:"year"`
				TraktID int    `json:"trakt_id"`
				AddedAt string `json:"added_at"`
			}
			var items []jsonItem
			for _, v := range resp {
				switch v.Type {
				case "movie":
					if v.Movie != nil {
						items = append(items, jsonItem{Type: "movie", Title: v.Movie.Title, Year: v.Movie.Year, TraktID: v.Movie.Ids.Trakt, AddedAt: v.ListedAt.Format(time.RFC3339)})
					}
				case "show":
					if v.Show != nil {
						items = append(items, jsonItem{Type: "show", Title: v.Show.Title, Year: v.Show.Year, TraktID: v.Show.Ids.Trakt, AddedAt: v.ListedAt.Format(time.RFC3339)})
					}
				}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"items": items, "page": pagination.Page, "page_count": pagination.PageCount, "item_count": pagination.ItemCount})
			return
		}

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{
			termenv.String("Type").Bold(),
			termenv.String("Title").Bold(),
			termenv.String("Added").Bold(),
		})
		for _, v := range resp {
			switch v.Type {
			case "movie":
				if v.Movie != nil {
					p := termenv.ColorProfile()
					year := termenv.String(fmt.Sprintf("(%d)", v.Movie.Year)).Foreground(p.Color("#B9BFCA"))
					t.AppendRow([]interface{}{
						"Movie 🎬",
						fmt.Sprintf("%s %s", v.Movie.Title, year),
						timediff.TimeDiff(v.ListedAt),
					})
				}
			case "show":
				if v.Show != nil {
					p := termenv.ColorProfile()
					year := termenv.String(fmt.Sprintf("(%d)", v.Show.Year)).Foreground(p.Color("#B9BFCA"))
					t.AppendRow([]interface{}{
						"TV Show 📺",
						fmt.Sprintf("%s %s", v.Show.Title, year),
						timediff.TimeDiff(v.ListedAt),
					})
				}
			}
		}

		t.SetStyle(table.StyleRounded)
		t.Render()

		fmt.Printf("Page %s out of %s, %s items in total", pagination.Page, pagination.PageCount, pagination.ItemCount)
	},
}

func init() {
	rootCmd.AddCommand(watchlistCmd)

	watchlistCmd.Flags().Int("page", 1, "")
	watchlistCmd.Flags().Int("limit", 10, "")
	watchlistCmd.Flags().String("type", "", "Filter by type (movies, shows)")
}
