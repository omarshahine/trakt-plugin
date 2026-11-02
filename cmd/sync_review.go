package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/omarshahine/trakt-plugin/sources"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var syncApproveCmd = &cobra.Command{
	Use:   "approve <title>...",
	Short: "Approve titles held for review so future syncs add them",
	Long: `Add titles to the sync config's allow list (and drop them from deny).

Shows already in your Trakt history sync automatically. A show you have never
logged, and any movie, is held for review so someone else's viewing on a
shared account never lands in your history. Approving lets the next
` + "`trakt-cli sync`" + ` add it.`,
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return editSyncLists(cmd, args, "allow", "deny")
	},
}

var syncDenyCmd = &cobra.Command{
	Use:          "deny <title>...",
	Short:        "Never sync these titles (e.g. another household member's shows)",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return editSyncLists(cmd, args, "deny", "allow")
	},
}

func init() {
	syncCmd.AddCommand(syncApproveCmd, syncDenyCmd)
}

// editSyncLists appends titles to list `add` and removes them from list
// `remove`, editing the YAML node tree so comments and ordering survive.
func editSyncLists(cmd *cobra.Command, titles []string, add, remove string) error {
	path, _ := cmd.Flags().GetString("config")
	if path == "" {
		path = sources.ConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s (create it first; see `trakt-cli sync --help`): %w", path, err)
	}
	out, changed, err := updateSyncLists(data, titles, add, remove)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if changed {
		if err := os.WriteFile(path, out, 0o600); err != nil {
			return err
		}
	}
	for _, t := range titles {
		fmt.Printf("%s: %s\n", add, t)
	}
	return nil
}

func updateSyncLists(data []byte, titles []string, add, remove string) ([]byte, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("sync config is not a YAML mapping")
	}
	root := doc.Content[0]
	list := func(key string, create bool) *yaml.Node {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == key {
				n := root.Content[i+1]
				if n.Kind != yaml.SequenceNode {
					n.Kind, n.Tag, n.Value, n.Content = yaml.SequenceNode, "!!seq", "", nil
				}
				return n
			}
		}
		if !create {
			return nil
		}
		k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
		v := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, k, v)
		return v
	}

	changed := false
	if rm := list(remove, false); rm != nil {
		kept := rm.Content[:0]
		for _, n := range rm.Content {
			drop := false
			for _, t := range titles {
				if sources.Normalize(n.Value) == sources.Normalize(t) {
					drop = true
				}
			}
			if drop {
				changed = true
			} else {
				kept = append(kept, n)
			}
		}
		rm.Content = kept
	}
	target := list(add, true)
	for _, t := range titles {
		present := false
		for _, n := range target.Content {
			if sources.Normalize(n.Value) == sources.Normalize(t) {
				present = true
			}
		}
		if !present {
			target.Content = append(target.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t})
			changed = true
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), changed, nil
}
