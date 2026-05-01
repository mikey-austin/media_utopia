// Package-level genre classifier.
//
// Maps fine-grained metadata (raw embedded tags, MusicBrainz/Discogs genres)
// to a fixed flat list of 15 top-level genres for browse-by-genre and search.
// The classifier prefers a local Ollama LLM for accuracy; when the LLM is
// unreachable, a static rollup map is used as a fallback at index-build time.
package fslibrary

import (
	"context"
	"fmt"
	"strings"
)

// ClassifyInput is the per-album input to a GenreClassifier.
type ClassifyInput struct {
	Artist        string
	Album         string
	TrackTitles   []string
	EmbeddedGenre string
	MBGenres      []string
}

// GenreClassifier classifies an album into one of genreAllowlist.
// Implementations may use a local LLM, a rule-based fallback, or both.
type GenreClassifier interface {
	Classify(ctx context.Context, in ClassifyInput) (string, error)
}

// promptGenerator is the minimal surface ollamaGenreClassifier needs from
// OllamaGenerator. Splitting it out lets tests use a stub.
type promptGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// ollamaGenreClassifier is a GenreClassifier backed by an Ollama text model.
type ollamaGenreClassifier struct {
	gen promptGenerator
}

// NewOllamaGenreClassifier wraps an OllamaGenerator. Returns nil if gen is nil.
func NewOllamaGenreClassifier(gen *OllamaGenerator) GenreClassifier {
	if gen == nil {
		return nil
	}
	return &ollamaGenreClassifier{gen: gen}
}

func (c *ollamaGenreClassifier) Classify(ctx context.Context, in ClassifyInput) (string, error) {
	prompt := buildGenrePrompt(in)
	raw, err := c.gen.Generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	return parseGenreResponse(raw), nil
}

// buildGenrePrompt formats the classification request as a single prompt.
func buildGenrePrompt(in ClassifyInput) string {
	var trackList string
	if len(in.TrackTitles) > 0 {
		n := len(in.TrackTitles)
		if n > 10 {
			n = 10
		}
		trackList = strings.Join(in.TrackTitles[:n], "; ")
	}
	mbStr := strings.Join(in.MBGenres, ", ")

	return fmt.Sprintf(`You are a music classifier. Given an album, return the single best top-level genre.

Valid genres (return EXACTLY one of these, with nothing else):
%s

Album:
Artist: %s
Title: %s
Sample track titles: %s
Embedded genre tag: %s
Existing fine-grained genres: %s

Reply with just the genre name. No explanation.`,
		strings.Join(genreAllowlist, ", "),
		in.Artist, in.Album, trackList, in.EmbeddedGenre, mbStr)
}

// genreAllowlist is the fixed taxonomy. Order is the order shown to the LLM
// in the prompt and the order asserted by tests.
var genreAllowlist = []string{
	"Classical",
	"Jazz",
	"Rock",
	"Pop",
	"Hip-Hop",
	"Electronic",
	"Folk",
	"Country",
	"Metal",
	"R&B/Soul",
	"Blues",
	"Reggae",
	"World",
	"Soundtrack",
	"Other",
}

// genreLLMSynonyms maps lowercased free-form text to the canonical genreAllowlist
// entry. Used by parseGenreResponse to tolerate small variations in LLM output
// (e.g., "soul" → "R&B/Soul"). Does NOT cover the full embedded-tag fallback
// vocabulary — that lives in genreRollup.
var genreLLMSynonyms = map[string]string{
	"classical":  "Classical",
	"jazz":       "Jazz",
	"rock":       "Rock",
	"pop":        "Pop",
	"hip-hop":    "Hip-Hop",
	"hip hop":    "Hip-Hop",
	"hiphop":     "Hip-Hop",
	"rap":        "Hip-Hop",
	"electronic": "Electronic",
	"folk":       "Folk",
	"country":    "Country",
	"metal":      "Metal",
	"r&b":        "R&B/Soul",
	"rnb":        "R&B/Soul",
	"soul":       "R&B/Soul",
	"r&b/soul":   "R&B/Soul",
	"blues":      "Blues",
	"reggae":     "Reggae",
	"world":      "World",
	"soundtrack": "Soundtrack",
	"score":      "Soundtrack",
	"other":      "Other",
}

// parseGenreResponse normalizes raw LLM output to one of genreAllowlist.
//
// Strategy:
//  1. Strip whitespace and surrounding punctuation.
//  2. Try an exact (case-insensitive) match against genreAllowlist.
//  3. Try a synonym match against the whole string, then per-word.
//  4. Try a substring scan for any allowlist entry within the text.
//  5. Otherwise return "Other".
func parseGenreResponse(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Other"
	}
	s = strings.Trim(s, ".\"' \t\n\r,;:")
	lower := strings.ToLower(s)

	for _, g := range genreAllowlist {
		if strings.EqualFold(s, g) {
			return g
		}
	}

	if g, ok := genreLLMSynonyms[lower]; ok {
		return g
	}
	for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '.' || r == ',' || r == ';' || r == ':'
	}) {
		if g, ok := genreLLMSynonyms[w]; ok {
			return g
		}
	}

	for _, g := range genreAllowlist {
		if g == "Other" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(g)) {
			return g
		}
	}

	return "Other"
}

// genreRollup maps lowercased fine-grained genre/tag strings to a top-level
// allowlist entry. Used when the LLM is unavailable. Each pattern is matched
// as a substring (case-insensitive), so "Alternative Rock" hits "rock". Order
// matters: more specific patterns must precede more general ones (e.g.,
// "deep house" before "house").
var genreRollup = []struct {
	pattern string
	target  string
}{
	// Classical
	{"baroque", "Classical"},
	{"romantic", "Classical"},
	{"chamber", "Classical"},
	{"symphony", "Classical"},
	{"symphonic", "Classical"},
	{"opera", "Classical"},
	{"concerto", "Classical"},
	{"sonata", "Classical"},
	{"early music", "Classical"},
	{"medieval", "Classical"},
	{"renaissance", "Classical"},
	{"orchestral", "Classical"},
	{"choral", "Classical"},
	{"classical", "Classical"},

	// Jazz
	{"bebop", "Jazz"},
	{"post-bop", "Jazz"},
	{"hard bop", "Jazz"},
	{"big band", "Jazz"},
	{"smooth jazz", "Jazz"},
	{"free jazz", "Jazz"},
	{"swing", "Jazz"},
	{"fusion", "Jazz"},
	{"jazz", "Jazz"},

	// Rock
	{"shoegaze", "Rock"},
	{"grunge", "Rock"},
	{"punk", "Rock"},
	{"hardcore", "Rock"},
	{"emo", "Rock"},
	{"indie", "Rock"},
	{"alternative", "Rock"},
	{"prog", "Rock"},
	{"psychedelic", "Rock"},
	{"garage", "Rock"},
	{"rock", "Rock"},

	// Pop (must precede Hip-Hop "hip-hop pop"-style edge cases? unlikely)
	{"k-pop", "Pop"},
	{"j-pop", "Pop"},
	{"synth-pop", "Pop"},
	{"synthpop", "Pop"},
	{"dance-pop", "Pop"},
	{"electropop", "Pop"},
	{"pop", "Pop"},

	// Hip-Hop
	{"hip-hop", "Hip-Hop"},
	{"hip hop", "Hip-Hop"},
	{"gangsta rap", "Hip-Hop"},
	{"trap", "Hip-Hop"},
	{"rap", "Hip-Hop"},

	// Electronic
	{"trance", "Electronic"},
	{"techno", "Electronic"},
	{"deep house", "Electronic"},
	{"house music", "Electronic"},
	{"house", "Electronic"},
	{"drum and bass", "Electronic"},
	{"dubstep", "Electronic"},
	{"ambient", "Electronic"},
	{"idm", "Electronic"},
	{"breakbeat", "Electronic"},
	{"electronica", "Electronic"},
	{"electronic", "Electronic"},

	// Folk
	{"singer-songwriter", "Folk"},
	{"acoustic", "Folk"},
	{"americana", "Folk"},
	{"bluegrass", "Folk"},
	{"folk", "Folk"},

	// Country
	{"honky-tonk", "Country"},
	{"country", "Country"},

	// Metal
	{"black metal", "Metal"},
	{"death metal", "Metal"},
	{"doom metal", "Metal"},
	{"thrash", "Metal"},
	{"metalcore", "Metal"},
	{"metal", "Metal"},

	// R&B/Soul
	{"r&b", "R&B/Soul"},
	{"rnb", "R&B/Soul"},
	{"neo-soul", "R&B/Soul"},
	{"motown", "R&B/Soul"},
	{"soul", "R&B/Soul"},
	{"funk", "R&B/Soul"},

	// Blues
	{"delta blues", "Blues"},
	{"chicago blues", "Blues"},
	{"blues", "Blues"},

	// Reggae
	{"dancehall", "Reggae"},
	{"reggae", "Reggae"},
	{"dub", "Reggae"},
	{"ska", "Reggae"},

	// World
	{"flamenco", "World"},
	{"afrobeat", "World"},
	{"bossa nova", "World"},
	{"celtic", "World"},
	{"latin", "World"},
	{"world", "World"},

	// Soundtrack
	{"film score", "Soundtrack"},
	{"video game", "Soundtrack"},
	{"soundtrack", "Soundtrack"},
	{"score", "Soundtrack"},
}

// rollupGenre returns a genreAllowlist entry for the given raw text by
// substring-matching against genreRollup. Returns "" if no match.
func rollupGenre(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	for _, e := range genreRollup {
		if strings.Contains(s, e.pattern) {
			return e.target
		}
	}
	return ""
}

// rollupGenreFromCandidates tries each candidate string in order, returning
// the first non-empty rollup match.
func rollupGenreFromCandidates(candidates []string) string {
	for _, c := range candidates {
		if g := rollupGenre(c); g != "" {
			return g
		}
	}
	return ""
}
