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

func TestRollupGenre(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"baroque", "Classical"},
		{"Romantic", "Classical"},
		{"Chamber Music", "Classical"},
		{"Symphony", "Classical"},
		{"early music", "Classical"},
		{"opera", "Classical"},
		{"bebop", "Jazz"},
		{"swing", "Jazz"},
		{"post-bop", "Jazz"},
		{"big band", "Jazz"},
		{"shoegaze", "Rock"},
		{"grunge", "Rock"},
		{"alternative rock", "Rock"},
		{"indie", "Rock"},
		{"trance", "Electronic"},
		{"techno", "Electronic"},
		{"deep house", "Electronic"},
		{"trap", "Hip-Hop"},
		{"gangsta rap", "Hip-Hop"},
		{"film score", "Soundtrack"},
		{"video game music", "Soundtrack"},
		{"unknown nonsense", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := rollupGenre(tc.in)
			if got != tc.want {
				t.Fatalf("rollupGenre(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRollupGenreFromCandidates(t *testing.T) {
	if got := rollupGenreFromCandidates([]string{"", "baroque", "rock"}); got != "Classical" {
		t.Fatalf("got %q, want Classical", got)
	}
	if got := rollupGenreFromCandidates([]string{"", ""}); got != "" {
		t.Fatalf("empty candidates: got %q, want \"\"", got)
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
