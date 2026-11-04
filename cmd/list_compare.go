package cmd

import (
	"encoding/json"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newListCompareCmd() *cobra.Command {
	c := &cobra.Command{Use: "compare", Short: "Read owned lists and preview membership reconciliation without writes", Args: cobra.NoArgs, SilenceUsage: true}
	for _, f := range []string{"source", "destination"} {
		c.Flags().String(f, "", "Exact owned numeric list ID")
		_ = c.MarkFlagRequired(f)
	}
	c.Flags().Int("limit", 20, "Maximum proposal items per direction (1-100)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		o := api.ListCompareOptions{}
		o.Source, _ = cmd.Flags().GetString("source")
		o.Destination, _ = cmd.Flags().GetString("destination")
		o.Limit, _ = cmd.Flags().GetInt("limit")
		if err := api.ValidateListCompare(o); err != nil {
			return err
		}
		client := api.NewAPIClient()
		r, err := client.CompareLists(o)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
	}
	return c
}
