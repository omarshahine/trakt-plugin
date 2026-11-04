package cmd

import (
	"bytes"
	"testing"
)

func TestShortlistDefaultsAndInvalidOptions(t *testing.T) {
	c := newShortlistCmd()
	w, _ := c.Flags().GetBool("exclude-watched")
	q, _ := c.Flags().GetBool("exclude-watchlisted")
	limit, _ := c.Flags().GetInt("limit")
	if !w || !q || limit != 20 {
		t.Fatal("unexpected shortlist defaults")
	}
	for _, args := range [][]string{{"--count", "0"}, {"--max-pages", "21"}, {"--mode", "related"}, {"--runtime-max", "0"}, {"--apply"}} {
		c := newShortlistCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatal("invalid shortlist accepted", args)
		}
	}
}
