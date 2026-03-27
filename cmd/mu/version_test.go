package main

import (
	"testing"
)

func TestVersionVars(t *testing.T) {
	// Default values should be set
	if version == "" {
		t.Error("version should not be empty")
	}
	if commit == "" {
		t.Error("commit should not be empty")
	}
	if date == "" {
		t.Error("date should not be empty")
	}
}
