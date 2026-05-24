package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

// refreshOutput is the --json payload for scripts/launchd consumers.
type refreshOutput struct {
	OK           bool   `json:"ok"`
	RefreshedAt  string `json:"refreshed_at"`
	UserSlug     string `json:"user_slug,omitempty"`
	HasRefresh   bool   `json:"has_refresh_token"`
	Error        string `json:"error,omitempty"`
	NextRunHint  string `json:"next_run_hint,omitempty"`
}

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh the Trakt OAuth access token using the stored refresh token",
	Long: `Exchange the stored refresh_token (~/.trakt.yaml) for a fresh access_token
without re-running the device-code flow. Trakt rotates the refresh_token on
each refresh, so this command re-persists both fields atomically.

Returns exit 0 on success, exit 1 if no refresh_token is stored or the refresh
is rejected (e.g. the app was revoked in Trakt's Connected Apps UI — in that
case, run 'trakt-cli auth' to bootstrap again).

Designed to be called by a weekly launchd job so the access token never
expires unattended. The doRequest path also auto-refreshes transparently on
401, so this command is belt-and-suspenders — useful for predictable refresh
cadence and audit logging.`,
	Run: func(cmd *cobra.Command, args []string) {
		jsonOut, _ := cmd.Flags().GetBool("json")

		client := api.NewAPIClient()
		out := refreshOutput{
			RefreshedAt: time.Now().UTC().Format(time.RFC3339),
			HasRefresh:  client.Credentials.RefreshToken != "",
		}

		if err := client.RefreshAccessToken(); err != nil {
			out.OK = false
			out.Error = err.Error()
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(out)
			} else {
				fmt.Fprintf(os.Stderr, "trakt-cli refresh failed: %v\n", err)
				if client.Credentials.RefreshToken == "" {
					fmt.Fprintln(os.Stderr, "hint: run 'trakt-cli auth' to bootstrap with a refresh token persisted")
				}
			}
			os.Exit(1)
		}

		out.OK = true
		out.HasRefresh = client.Credentials.RefreshToken != ""
		// Best-effort slug — useful for confirming the token actually works.
		if slug, slugErr := client.GetUserSlug(); slugErr == nil {
			out.UserSlug = slug
		}
		// Trakt access tokens are 90 days; recommend refresh well inside that.
		out.NextRunHint = "schedule weekly via launchd; current token valid for ~90 days"

		if jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(out)
		} else {
			fmt.Printf("Refreshed Trakt OAuth token at %s (user=%s)\n", out.RefreshedAt, out.UserSlug)
		}
	},
}

func init() {
	rootCmd.AddCommand(refreshCmd)
}
