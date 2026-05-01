package fslibrary

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubGenerator struct {
	response   string
	err        error
	lastPrompt string
}

func (s *stubGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	s.lastPrompt = prompt
	return s.response, s.err
}

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

func TestBuildGenrePrompt(t *testing.T) {
	in := ClassifyInput{
		Artist:        "Glenn Gould",
		Album:         "Bach: Goldberg Variations",
		TrackTitles:   []string{"Aria", "Variation 1"},
		EmbeddedGenre: "Classical",
		MBGenres:      []string{"baroque", "early music"},
	}
	p := buildGenrePrompt(in)
	if !strings.Contains(p, "Glenn Gould") {
		t.Fatal("prompt missing artist")
	}
	if !strings.Contains(p, "Bach: Goldberg Variations") {
		t.Fatal("prompt missing album")
	}
	if !strings.Contains(p, "baroque") {
		t.Fatal("prompt missing MBGenres")
	}
	for _, g := range genreAllowlist {
		if !strings.Contains(p, g) {
			t.Fatalf("prompt missing allowlist entry %q", g)
		}
	}
}

func TestOllamaClassifierClassifyHappyPath(t *testing.T) {
	gen := &stubGenerator{response: "Classical"}
	c := &ollamaGenreClassifier{gen: gen}
	got, err := c.Classify(context.Background(), ClassifyInput{Artist: "Bach", Album: "Mass in B Minor"})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got != "Classical" {
		t.Fatalf("got %q, want Classical", got)
	}
	if gen.lastPrompt == "" {
		t.Fatal("prompt was not sent")
	}
}

func TestOllamaClassifierClassifyError(t *testing.T) {
	gen := &stubGenerator{err: errors.New("boom")}
	c := &ollamaGenreClassifier{gen: gen}
	got, err := c.Classify(context.Background(), ClassifyInput{Artist: "X", Album: "Y"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != "" {
		t.Fatalf("got %q, want \"\"", got)
	}
}

func TestOllamaClassifierUnparseableResponse(t *testing.T) {
	gen := &stubGenerator{response: "I cannot determine that."}
	c := &ollamaGenreClassifier{gen: gen}
	got, err := c.Classify(context.Background(), ClassifyInput{Artist: "X", Album: "Y"})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got != "Other" {
		t.Fatalf("got %q, want Other (parser fallback)", got)
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
