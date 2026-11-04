package cmd

import (
	"bytes"
	"testing"
)

func TestDiscoveryCLIRejectsBadInputsBeforeCredentials(t *testing.T) {
	for _, args := range [][]string{{}, {"unrelated"}, {"popular", "--type", "person"}, {"related"}, {"related", "--id", "55/../../settings"}, {"recommendations", "--page", "2"}, {"trending", "--limit", "101"}, {"popular", "--id", "55"}} {
		c := newDiscoverCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatal("invalid CLI accepted", args)
		}
	}
}
