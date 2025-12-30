package core

import "testing"

func TestErrorForReplyCode(t *testing.T) {
	tests := []struct {
		code     string
		expected int
	}{
		{"LEASE_REQUIRED", ExitLease},
		{"LEASE_MISMATCH", ExitLease},
		{"NOT_FOUND", ExitNotFound},
		{"CONFLICT", ExitConflict},
		{"INVALID", ExitUsage},
		{"UNKNOWN", ExitRuntime},
	}

	for _, test := range tests {
		err := ErrorForReplyCode(test.code, "message")
		if err.Code != test.expected {
			t.Fatalf("code %s expected %d got %d", test.code, test.expected, err.Code)
		}
	}
}
