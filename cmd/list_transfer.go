package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

func newListTransferCmd(action string) *cobra.Command {
	c := &cobra.Command{Use: action, Short: "Preview or explicitly " + action + " selected memberships between owned lists", Args: cobra.NoArgs, SilenceUsage: true}
	for _, name := range []string{"source", "destination", "type"} {
		c.Flags().String(name, "", "Required "+name)
		_ = c.MarkFlagRequired(name)
	}
	c.Flags().StringSlice("ids", nil, "Exact Trakt MEDIA IDs (1-100)")
	_ = c.MarkFlagRequired("ids")
	c.Flags().Bool("apply", false, "Execute authorized transfer; default offline preview")
	c.Flags().String("expect-user", "", "Expected token/list owner; required with apply")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		source, _ := cmd.Flags().GetString("source")
		dest, _ := cmd.Flags().GetString("destination")
		kind, _ := cmd.Flags().GetString("type")
		ids, _ := cmd.Flags().GetStringSlice("ids")
		x := api.ListTransfer{Action: action, Source: source, Destination: dest, Type: kind, IDs: ids}
		p, err := api.PreviewListTransfer(x)
		if err != nil {
			return err
		}
		apply, _ := cmd.Flags().GetBool("apply")
		if !apply {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(p)
		}
		expected, _ := cmd.Flags().GetString("expect-user")
		if expected == "" {
			return fmt.Errorf("expect-user required with apply")
		}
		client := api.NewAPIClient()
		r, applyErr := client.ApplyListTransfer(expected, x)
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(r); err != nil {
			return err
		}
		return applyErr
	}
	return c
}
