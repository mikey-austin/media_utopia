package main

import (
	"testing"
)

func TestSelectorArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no_args", nil, ""},
		{"empty_slice", []string{}, ""},
		{"one_arg", []string{"living-room"}, "living-room"},
		{"two_args", []string{"living-room", "extra"}, "living-room"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectorArg(tt.args)
			if got != tt.want {
				t.Errorf("selectorArg(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestDefaultIdentity(t *testing.T) {
	tests := []struct {
		name    string
		flagVal string
		cfgVal  string
		wantNot string // value it should NOT be (we can't predict hostname)
	}{
		{"flag_takes_precedence", "flag-user", "config-user", ""},
		{"config_fallback", "", "config-user", ""},
		{"auto_generated", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultIdentity(tt.flagVal, tt.cfgVal)
			if got == "" {
				t.Error("defaultIdentity should never return empty string")
			}
			switch tt.name {
			case "flag_takes_precedence":
				if got != "flag-user" {
					t.Errorf("expected flag-user, got %q", got)
				}
			case "config_fallback":
				if got != "config-user" {
					t.Errorf("expected config-user, got %q", got)
				}
			case "auto_generated":
				// Should return user@host or host or "mu-unknown"
				if got == "" {
					t.Error("should generate an identity")
				}
			}
		})
	}
}

func TestNormalizeResolve(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"auto", "auto", false},
		{"yes", "yes", false},
		{"no", "no", false},
		{"", "auto", false},
		{"AUTO", "auto", false},
		{"Yes", "yes", false},
		{"  no  ", "no", false},
		{"invalid", "", true},
		{"maybe", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := normalizeResolve(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("normalizeResolve(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("normalizeResolve(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeItem(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"mu:renderer:foo", true},
		{"playlist:my-list", true},
		{"http://example.com/song.mp3", true},
		{"https://example.com/song.mp3", true},
		{"living-room", false},
		{"", false},
		{"ftp://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			got := looksLikeItem(tt.arg)
			if got != tt.want {
				t.Errorf("looksLikeItem(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestLooksLikeVolume(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"50", true},
		{"+10", true},
		{"-5", true},
		{"0", true},
		{"100", true},
		{"", false},
		{"living-room", false},
		{"abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			got := looksLikeVolume(tt.arg)
			if got != tt.want {
				t.Errorf("looksLikeVolume(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}
