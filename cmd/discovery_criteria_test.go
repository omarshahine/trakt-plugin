package cmd

import (
	"bytes"
	"testing"
)

func TestCriteriaCLIRefusesInvalidRangesBeforeCredentials(t *testing.T) {
	for _, extra := range [][]string{{"--year-min", "2025", "--year-max", "2000"}, {"--runtime-max", "0"}, {"--min-rating", "NaN"}, {"--min-votes", "-1"}, {"--genres", "../settings"}} {
		c := newDiscoverCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(append([]string{"popular"}, extra...))
		if err := c.Execute(); err == nil {
			t.Fatal("invalid criteria accepted", extra)
		}
	}
	c := newGenresCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--type", "person"})
	if err := c.Execute(); err == nil {
		t.Fatal("bad genre type")
	}
}
