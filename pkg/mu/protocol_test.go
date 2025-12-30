package mu

import "testing"

func TestValidateCommandEnvelopeLeaseRequired(t *testing.T) {
	cmd, err := NewCommand("playback.play", PlaybackPlayBody{})
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	cmd.ID = "id"
	cmd.TS = 1
	cmd.From = "tester"
	if err := ValidateCommandEnvelope(cmd); err == nil {
		t.Fatalf("expected lease error")
	}

	cmd.Lease = &Lease{SessionID: "s", Token: "t"}
	if err := ValidateCommandEnvelope(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCommandEnvelopeMissingFields(t *testing.T) {
	cmd := CommandEnvelope{}
	if err := ValidateCommandEnvelope(cmd); err == nil {
		t.Fatalf("expected error")
	}
}
