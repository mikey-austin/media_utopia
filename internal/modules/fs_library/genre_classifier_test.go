package fslibrary

import "testing"

func TestParseGenreResponse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"exact match", "Classical", "Classical"},
		{"trailing newline", "Jazz\n", "Jazz"},
		{"surrounding prose", "The genre is Rock.", "Rock"},
		{"lowercased", "electronic", "Electronic"},
		{"mixed case", "hip-hop", "Hip-Hop"},
		{"r&b/soul forms", "R&B", "R&B/Soul"},
		{"r&b/soul forms 2", "Soul", "R&B/Soul"},
		{"unknown text", "Vaporwave", "Other"},
		{"empty", "", "Other"},
		{"refusal", "I'm sorry, I can't help with that.", "Other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGenreResponse(tc.in)
			if got != tc.want {
				t.Fatalf("parseGenreResponse(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGenreAllowlistContents(t *testing.T) {
	expected := []string{
		"Classical", "Jazz", "Rock", "Pop", "Hip-Hop", "Electronic",
		"Folk", "Country", "Metal", "R&B/Soul", "Blues", "Reggae",
		"World", "Soundtrack", "Other",
	}
	if len(genreAllowlist) != len(expected) {
		t.Fatalf("genreAllowlist length = %d, want %d", len(genreAllowlist), len(expected))
	}
	for i, g := range expected {
		if genreAllowlist[i] != g {
			t.Fatalf("genreAllowlist[%d] = %q, want %q", i, genreAllowlist[i], g)
		}
	}
}
