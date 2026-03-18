package cmd

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/briandowns/spinner"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

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

type Credentials struct {
	ClientID     string `yaml:"client-id"`
	ClientSecret string `yaml:"client-secret"`
	AccessToken  string `yaml:"access-token"`
}

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

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with trakt.tv",
	Long:  "Sign in with your trakt.tv account using the device code flow.",
	Run: func(cmd *cobra.Command, args []string) {
		client := api.NewAPIClient()
		clientID := resolveClientID(cmd)
		clientSecret := resolveClientSecret(cmd)

		resp, err := client.AuthDeviceCode(&api.AuthDeviceCodeReq{
			ClientID: clientID,
		})
		if err != nil {
			logrus.WithError(err).Fatal("Failed to get device code")
			return
		}

		fmt.Printf("Please go to %s and enter the following code: %s\n", resp.VerificationURL, resp.UserCode)

		s := spinner.New(spinner.CharSets[2], 100*time.Millisecond)
		s.Start()
		s.Prefix = "Waiting for authorisation... "

		for {
			tokenResp, err := client.AuthDeviceToken(&api.AuthDeviceTokenReq{
				Code:         resp.DeviceCode,
				ClientID:     clientID,
				ClientSecret: clientSecret,
			})
			if err != nil {
				logrus.WithError(err).Fatal("Failed to get device code")
				return
			}
			if len(tokenResp.AccessToken) == 0 {
				time.Sleep(time.Duration(resp.Interval) * time.Second)
			} else {
				creds := Credentials{
					ClientID:     clientID,
					ClientSecret: clientSecret,
					AccessToken:  tokenResp.AccessToken,
				}

				yamlData, err := yaml.Marshal(&creds)
				if err != nil {
					fmt.Printf("Error while Marshaling. %v", err)
				}

				homeDir, err := os.UserHomeDir()
				if err != nil {
					log.Fatal(err)
				}
				err = os.WriteFile(homeDir+"/.trakt.yaml", yamlData, 0644)
				if err != nil {
					fmt.Printf("Error while writing to file. %v", err)
				}

				s.Stop()
				fmt.Printf("Successfully authenticated, creds written to ~/.trakt.yaml\n")

				break
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.PersistentFlags().String("client-id", "", "Trakt API client ID (optional, uses built-in default)")
	authCmd.PersistentFlags().String("client-secret", "", "Trakt API client secret (optional, uses built-in default)")
}
