package cmd

import (
	"bytes"
	"testing"
)

func TestTitleCommandsRejectInvalidArgumentsBeforeCredentials(t *testing.T) {
	for _, args := range [][]string{{"details"}, {"details", "--id", "55", "--type", "person"}, {"seasons", "--id", "../settings"}, {"episodes", "--id", "55"}, {"episodes", "--id", "55", "--season", "-1"}, {"episode", "--id", "55", "--season", "0"}, {"episode", "--id", "55", "--season", "0", "--episode", "10001"}, {"details", "--id", "55", "--apply"}} {
		c := newTitlesCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatal("bad title command accepted", args)
		}
	}
}
