package proc

import (
	"testing"
    "strings"
)

func TestKillPort(t *testing.T) {
	// This test is very basic and primarily checks for unexpected errors.
	// A more robust test would involve starting a process on a known port,
	// then killing it and verifying it's no longer running.  However, that
	// is significantly more complex and platform-dependent.

	// We'll use a high, unlikely-to-be-used port for this test.
	err := KillPort("65000")
	if err != nil {
		// We don't *expect* an error, but "no process found" is acceptable.
		if !strings.Contains(err.Error(), "no process found") {
			t.Errorf("KillPort returned an unexpected error: %v", err)
		}
	}
}