package main

import (
	"testing"
)

func TestCompletionCommand(t *testing.T) {
	cmd := completionCommand()
	if cmd.Use != "completion [bash|zsh|fish|powershell]" {
		t.Errorf("unexpected Use: %s", cmd.Use)
	}
	if len(cmd.ValidArgs) != 4 {
		t.Errorf("expected 4 valid args, got %d", len(cmd.ValidArgs))
	}
}
