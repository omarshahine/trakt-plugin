package cmd

import (
	"bytes"
	"testing"
)

func TestSeededRelatedDefaultsAndOfflineValidation(t *testing.T) {
	c := newSeededRelatedCmd()
	threshold, _ := c.Flags().GetInt("min-seed-rating")
	excluded, _ := c.Flags().GetBool("exclude-watchlisted")
	if threshold != 8 || !excluded {
		t.Fatal("unsafe defaults")
	}
	for _, args := range [][]string{{"--count", "0"}, {"--max-seeds", "11"}, {"--per-seed", "21"}, {"--source", "unknown"}, {"--type", "episode"}, {"--apply"}, {"--min-seed-rating", "11"}} {
		c := newSeededRelatedCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatal("invalid input accepted", args)
		}
	}
}
