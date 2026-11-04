package cmd

import (
	"bytes"
	"testing"
)

func TestEpisodeQueueDefaultsAndOfflineValidation(t *testing.T) {
	c := newEpisodeQueueCmd()
	specials, _ := c.Flags().GetBool("include-specials")
	budget, _ := c.Flags().GetInt("max-shows")
	if specials || budget != 5 {
		t.Fatal("unsafe defaults")
	}
	for _, args := range [][]string{{"--count", "0"}, {"--max-shows", "21"}, {"--runtime-max", "0"}, {"--apply"}, {"extra"}} {
		c := newEpisodeQueueCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatal("invalid args accepted", args)
		}
	}
}
