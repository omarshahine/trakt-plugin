package cmd

import (
	"strings"
	"testing"
)

func TestBackupRejectsUnboundedTimeoutBeforeLoadingCredentials(t *testing.T) {
	flag := backupCmd.Flags().Lookup("timeout-seconds")
	if flag == nil || flag.DefValue != "600" {
		t.Fatalf("missing bounded default: %+v", flag)
	}
	original := flag.Value.String()
	defer func() { _ = flag.Value.Set(original) }()
	for _, value := range []string{"0", "-1", "86401"} {
		if err := flag.Value.Set(value); err != nil {
			t.Fatal(err)
		}
		// Validation must happen before even reading local credentials or networking.
		if err := backupCmd.RunE(backupCmd, nil); err == nil || !strings.Contains(err.Error(), "timeout-seconds must be between") {
			t.Fatalf("timeout %s accepted: %v", value, err)
		}
	}
}
