package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/mergestat/timediff"
	"github.com/muesli/termenv"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// calendarCmd shows upcoming episodes for shows the user tracks on Trakt.
// Unlike every other read command in this CLI, calendar data is
// FORWARD-looking — it enables proactive agent workflows (e.g., "new
// episode of X drops on Friday") that the rest of the tools can't support
// since they only describe current/past state.
var calendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "Show upcoming episodes of shows you watch",
	Long:  `Show upcoming episodes of shows on your watchlist/history, in a date window. Defaults to the next 7 days starting today.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()

		start, _ := cmd.Flags().GetString("start")
		days, _ := cmd.Flags().GetInt("days")
		onlyNew, _ := cmd.Flags().GetBool("new")

		// Validate start if provided — reject a malformed date up front so
		// the user sees a clear error instead of a 404 from Trakt.
		if start != "" {
			if _, err := time.Parse("2006-01-02", start); err != nil {
				logrus.Fatalf("Invalid --start format. Use YYYY-MM-DD (e.g. 2026-04-10)")
			}
		}

		// Fail fast on nonsensical windows. The API layer has a safety net
		// that resets <=0 to 7, but that silently rewrites user input —
		// better to reject it here so the mistake is visible.
		if days <= 0 {
			logrus.Fatalf("--days must be a positive integer (got %d)", days)
		}

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)
		if !jsonOutput {
			s.Prefix = "Loading calendar... "
			s.Start()
		}

		episodes, err := client.GetMyShowsCalendar(start, days, onlyNew)
		s.Stop()
		if err != nil {
			logrus.WithError(err).Fatal("Failed to load calendar")
		}

		if jsonOutput {
			type jsonItem struct {
				FirstAired  string `json:"first_aired"`
				ShowTitle   string `json:"show_title"`
				ShowYear    int    `json:"show_year"`
				ShowTraktID int    `json:"show_trakt_id"`
				Season      int    `json:"season"`
				Episode     int    `json:"episode"`
				Title       string `json:"title"`
			}
			items := make([]jsonItem, 0, len(episodes))
			for _, e := range episodes {
				items = append(items, jsonItem{
					FirstAired:  e.FirstAired.Format(time.RFC3339),
					ShowTitle:   e.Show.Title,
					ShowYear:    e.Show.Year,
					ShowTraktID: e.Show.Ids.Trakt,
					Season:      e.Episode.Season,
					Episode:     e.Episode.Number,
					Title:       e.Episode.Title,
				})
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]interface{}{
				"items": items,
				"count": len(items),
			})
			return
		}

		if len(episodes) == 0 {
			fmt.Println("No upcoming episodes in the window.")
			return
		}

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{
			termenv.String("Airs").Bold(),
			termenv.String("Show").Bold(),
			termenv.String("Episode").Bold(),
			termenv.String("Title").Bold(),
		})
		// Hoist terminal-capability detection out of the loop — it's
		// invariant across iterations.
		p := termenv.ColorProfile()
		for _, e := range episodes {
			year := termenv.String(fmt.Sprintf("(%d)", e.Show.Year)).Foreground(p.Color("#B9BFCA"))
			t.AppendRow([]interface{}{
				timediff.TimeDiff(e.FirstAired),
				fmt.Sprintf("%s %s", e.Show.Title, year),
				fmt.Sprintf("S%02dE%02d", e.Episode.Season, e.Episode.Number),
				e.Episode.Title,
			})
		}
		t.SetStyle(table.StyleRounded)
		t.Render()

		fmt.Printf("%d upcoming episodes\n", len(episodes))
	},
}

func init() {
	rootCmd.AddCommand(calendarCmd)
	calendarCmd.Flags().String("start", "", "Start date (YYYY-MM-DD, defaults to today)")
	calendarCmd.Flags().Int("days", 7, "Number of days to look ahead")
	calendarCmd.Flags().Bool("new", false, "Only show series premieres (S01E01 airings)")
}
