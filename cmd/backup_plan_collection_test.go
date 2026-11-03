package cmd

import (
	"bytes"
	"testing"
)

func TestCollectionPlanHasNoApplyMode(t *testing.T) {
	c := newBackupPlanCollectionCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--source", "unused", "--current", "unused", "--apply"})
	if err := c.Execute(); err == nil {
		t.Fatal("apply mode accepted")
	}
}
