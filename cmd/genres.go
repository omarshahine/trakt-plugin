package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newGenresCmd() *cobra.Command {
	c := &cobra.Command{Use: "genres", Short: "Read current Trakt genre names and slugs", Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().String("type", "show", "movie or show")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		kind, _ := cmd.Flags().GetString("type")
		if kind != "movie" && kind != "show" {
			return fmt.Errorf("type must be movie or show")
		}
		client := api.NewAPIClient()
		items, err := client.GetDiscoveryGenres(kind)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Type  string      `json:"type"`
			Items []api.Genre `json:"items"`
		}{kind, items})
	}
	return c
}
func init() { rootCmd.AddCommand(newGenresCmd()) }
