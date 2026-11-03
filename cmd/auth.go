package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/omarshahine/trakt-plugin/api"

	"github.com/spf13/cobra"
)

// Default OAuth app credentials (public client identifiers).
// These identify the app, not the user. The device code flow requires
// explicit user approval before any access token is issued.
// Override with --client-id / --client-secret flags or env vars.
const (
	defaultClientID     = "5d65a0599e8b55e466c1ebbe579e4f639ec66cdf9eede0c0b00dc30700d15319"
	defaultClientSecret = "79d434ac6d0f8df254ae654b546adbc26808fef2f3e7aa43a25e0c0fe402989c"
)

type Credentials = api.Credentials

// resolveClientID returns the client ID from flag, env var, or default.
func resolveClientID(cmd *cobra.Command) string {
	if v := cmd.Flag("client-id").Value.String(); v != "" {
		return v
	}
	if v := os.Getenv("TRAKT_CLIENT_ID"); v != "" {
		return v
	}
	return defaultClientID
}

// resolveClientSecret returns the client secret from flag, env var, or default.
func resolveClientSecret(cmd *cobra.Command) string {
	if v := cmd.Flag("client-secret").Value.String(); v != "" {
		return v
	}
	if v := os.Getenv("TRAKT_CLIENT_SECRET"); v != "" {
		return v
	}
	return defaultClientSecret
}

// authClientFactory is a local test seam; production always uses the normal
// Trakt endpoint. Tests replace its transport with an in-memory RoundTripper.
var authClientFactory = func() api.APIClient {
	return api.NewAPIClientWithCredentials(api.Credentials{})
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with trakt.tv",
	Long:  "Sign in with the device code flow. With --json, return verification instructions immediately; poll once with --json --session HANDLE. JSON session storage is unavailable on Windows; use plain auth locally.",
	RunE:  runAuth,
}

func runAuth(cmd *cobra.Command, _ []string) error {
	jsonOut, _ := cmd.Flags().GetBool("json")
	session, _ := cmd.Flags().GetString("session")
	if session != "" && !jsonOut {
		return fmt.Errorf("--session requires --json")
	}
	client := authClientFactory()
	if jsonOut {
		var result authOutput
		var err error
		if session == "" {
			result, err = startAuthSession(&client, resolveClientID(cmd), resolveClientSecret(cmd))
		} else {
			result, err = pollAuthSession(&client, session)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}

	clientID, clientSecret := resolveClientID(cmd), resolveClientSecret(cmd)
	resp, err := client.AuthDeviceCode(&api.AuthDeviceCodeReq{ClientID: clientID})
	if err != nil || !publicDeviceResponse(resp, clientSecret) {
		return fmt.Errorf("failed to start Trakt authentication")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Please go to %s and enter the following code: %s\n", resp.VerificationURL, resp.UserCode)
	indicator := spinner.New(spinner.CharSets[2], 100*time.Millisecond)
	indicator.Prefix = "Waiting for authorisation... "
	indicator.Start()
	defer indicator.Stop()
	deadline := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		token, err := client.AuthDeviceToken(&api.AuthDeviceTokenReq{Code: resp.DeviceCode, ClientID: clientID, ClientSecret: clientSecret})
		if err != nil {
			return fmt.Errorf("Trakt authentication failed")
		}
		if token.AccessToken != "" {
			if err := persistAuthCredentials(api.Credentials{ClientID: clientID, ClientSecret: clientSecret, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}); err != nil {
				return err
			}
			indicator.Stop()
			fmt.Fprintln(cmd.OutOrStdout(), "Successfully authenticated, creds written to ~/.trakt.yaml")
			return nil
		}
		timer := time.NewTimer(time.Duration(resp.Interval) * time.Second)
		select {
		case <-cmd.Context().Done():
			timer.Stop()
			return fmt.Errorf("Trakt authentication cancelled")
		case <-timer.C:
		}
	}
	return fmt.Errorf("Trakt authentication expired; start again")
}

func persistAuthCredentials(creds api.Credentials) error {
	// User-slug caching remains lazy in GetUserSlug. Complete authentication
	// after its one token request and durable credential write.
	if err := api.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to persist Trakt credentials")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.PersistentFlags().String("client-id", "", "Trakt API client ID (optional, uses built-in default)")
	authCmd.PersistentFlags().String("client-secret", "", "Trakt API client secret (optional, uses built-in default)")
	authCmd.Flags().String("session", "", "Poll one pending private authentication session (requires --json)")
}
