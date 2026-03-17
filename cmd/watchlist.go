package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/angristan/trakt-cli/api"
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
		s.Start()
		s.Prefix = "Loading watchlist... "

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

		s.Stop()

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
