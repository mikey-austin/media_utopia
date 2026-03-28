package main

import (
	"testing"
)

func TestVersionCommand(t *testing.T) {
	cmd := versionCommand()
	if cmd.Use != "version" {
		t.Errorf("unexpected Use: %s", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

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
