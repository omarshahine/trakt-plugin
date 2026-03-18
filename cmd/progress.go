package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/briandowns/spinner"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/muesli/termenv"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type ProgressItem struct {
	Title     string `json:"title"`
	Year      int    `json:"year"`
	TraktID   int    `json:"trakt_id"`
	Aired     int    `json:"aired"`
	Watched   int    `json:"watched"`
	Remaining int    `json:"remaining"`
	Percent   int    `json:"percent"`
	Status    string `json:"status"`
	NextEp    string `json:"next_episode,omitempty"`
}

var progressCmd = &cobra.Command{
	Use:   "progress",
	Short: "Show watch progress for watchlist shows",
	Long: `Compare your watchlist shows against your watch history to find
shows that are in progress (started but not finished), not started,
or completed but still on the watchlist.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()
		showAll, _ := cmd.Flags().GetBool("all")

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)
		if !jsonOutput {
			s.Start()
			s.Prefix = "Loading watchlist... "
		}

		settings, err := client.GetUserSettings()
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get user settings")
		}

		// Collect unique show IDs from both watchlist and watched history
		type showInfo struct {
			Title   string
			Year    int
			TraktID int
		}
		showMap := make(map[int]showInfo)

		// Get all watchlist shows (up to 100)
		watchlist, _, err := client.GetUserWatchlist(settings.User.Ids.Slug, "shows", api.PaginationsParams{
			Limit: 100,
		})
		if err != nil {
			s.Stop()
			logrus.WithError(err).Fatal("Failed to get watchlist")
		}

		for _, item := range watchlist {
			if item.Show == nil {
				continue
			}
			showMap[item.Show.Ids.Trakt] = showInfo{
				Title:   item.Show.Title,
				Year:    item.Show.Year,
				TraktID: item.Show.Ids.Trakt,
			}
		}

		// Also get all shows with any watch history
		s.Prefix = "Loading watched shows... "
		watched, err := client.GetUserWatched(settings.User.Ids.Slug, "shows")
		if err != nil {
			logrus.WithError(err).Warn("Failed to get watched shows, continuing with watchlist only")
		} else {
			for _, item := range watched {
				if item.Show == nil {
					continue
				}
				if _, exists := showMap[item.Show.Ids.Trakt]; !exists {
					showMap[item.Show.Ids.Trakt] = showInfo{
						Title:   item.Show.Title,
						Year:    item.Show.Year,
						TraktID: item.Show.Ids.Trakt,
					}
				}
			}
		}

		s.Prefix = "Checking progress... "

		var inProgress, notStarted, completed []ProgressItem

		for _, show := range showMap {
			progress, err := client.GetShowProgress(show.TraktID)
			if err != nil {
				logrus.WithError(err).Warnf("Failed to get progress for %s", show.Title)
				continue
			}

			pi := ProgressItem{
				Title:   show.Title,
				Year:    show.Year,
				TraktID: show.TraktID,
				Aired:   progress.Aired,
				Watched: progress.Completed,
			}

			if progress.Aired > 0 {
				pi.Remaining = progress.Aired - progress.Completed
				pi.Percent = int(float64(progress.Completed) / float64(progress.Aired) * 100)
			}

			if progress.NextEpisode != nil {
				pi.NextEp = fmt.Sprintf("S%02dE%02d", progress.NextEpisode.Season, progress.NextEpisode.Number)
			}

			if progress.Completed == 0 {
				pi.Status = "not_started"
				notStarted = append(notStarted, pi)
			} else if progress.Completed >= progress.Aired && progress.Aired > 0 {
				pi.Status = "completed"
				completed = append(completed, pi)
			} else {
				pi.Status = "in_progress"
				inProgress = append(inProgress, pi)
			}
		}

		s.Stop()

		if jsonOutput {
			output := map[string]interface{}{
				"in_progress": inProgress,
				"not_started": notStarted,
				"completed":   completed,
			}
			if !showAll {
				output = map[string]interface{}{
					"in_progress": inProgress,
					"summary": map[string]int{
						"in_progress": len(inProgress),
						"not_started": len(notStarted),
						"completed":   len(completed),
					},
				}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(output)
			return
		}

		// Table output
		renderProgressSection("IN PROGRESS", inProgress, true)

		if showAll {
			renderProgressSection("NOT STARTED", notStarted, false)
			renderProgressSection("COMPLETED (still on watchlist)", completed, false)
		} else {
			fmt.Printf("\n%d not started, %d completed — use --all to see\n",
				len(notStarted), len(completed))
		}
	},
}

func renderProgressSection(title string, items []ProgressItem, showNext bool) {
	if len(items) == 0 {
		fmt.Printf("\n%s: none\n", title)
		return
	}

	fmt.Printf("\n%s (%d):\n", title, len(items))

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)

	if showNext {
		t.AppendHeader(table.Row{
			termenv.String("Title").Bold(),
			termenv.String("Progress").Bold(),
			termenv.String("Next").Bold(),
		})
		for _, item := range items {
			t.AppendRow([]interface{}{
				fmt.Sprintf("%s (%d)", item.Title, item.Year),
				fmt.Sprintf("%d/%d (%d%%)", item.Watched, item.Aired, item.Percent),
				item.NextEp,
			})
		}
	} else {
		t.AppendHeader(table.Row{
			termenv.String("Title").Bold(),
			termenv.String("Episodes").Bold(),
		})
		for _, item := range items {
			eps := fmt.Sprintf("%d", item.Aired)
			if item.Aired == 0 {
				eps = "?"
			}
			t.AppendRow([]interface{}{
				fmt.Sprintf("%s (%d)", item.Title, item.Year),
				eps,
			})
		}
	}

	t.SetStyle(table.StyleRounded)
	t.Render()
}

func init() {
	rootCmd.AddCommand(progressCmd)
	progressCmd.Flags().Bool("all", false, "Show all categories (in progress, not started, completed)")
}
