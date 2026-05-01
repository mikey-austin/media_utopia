// Package-level genre classifier.
//
// Maps fine-grained metadata (raw embedded tags, MusicBrainz/Discogs genres)
// to a fixed flat list of 15 top-level genres for browse-by-genre and search.
// The classifier prefers a local Ollama LLM for accuracy; when the LLM is
// unreachable, a static rollup map is used as a fallback at index-build time.
package fslibrary

import "strings"

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
