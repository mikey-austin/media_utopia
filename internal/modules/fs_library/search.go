package fslibrary

import (
	"strings"
	"sync"
)

// foldRunes maps lowercase Latin letters with diacritics (and a few
// ligatures) to their ASCII equivalents so "motorhead" matches "Motörhead"
// without pulling in a normalization dependency.
var foldRunes = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i", 'ı': "i",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s",
	'ź': "z", 'ż': "z", 'ž': "z",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ł': "l",
	'ŕ': "r", 'ŗ': "r", 'ř': "r",
	'ţ': "t", 'ť': "t",
	'ĥ': "h", 'ĵ': "j", 'ķ': "k", 'ŵ': "w",
	'đ': "d", 'ð': "d", 'þ': "th",
	'ß': "ss", 'æ': "ae", 'œ': "oe",
}

// foldString lowercases s and strips common diacritics. ASCII input takes a
// fast path with a single allocation.
func foldString(s string) string {
	lower := strings.ToLower(s)
	ascii := true
	for i := 0; i < len(lower); i++ {
		if lower[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return lower
	}
	var b strings.Builder
	b.Grow(len(lower))
	for _, r := range lower {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		if rep, ok := foldRunes[r]; ok {
			b.WriteString(rep)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// queryEmbedCacheSize bounds the per-module cache of query embeddings.
const queryEmbedCacheSize = 256

// queryEmbedCache memoizes query-embedding vectors so repeated searches
// (and search-as-you-type re-issues) don't each pay a remote HTTP call.
type queryEmbedCache struct {
	mu    sync.Mutex
	m     map[string][]float32
	order []string // insertion order for FIFO eviction
}

func (c *queryEmbedCache) get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *queryEmbedCache) put(key string, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string][]float32)
	}
	if _, exists := c.m[key]; !exists {
		c.order = append(c.order, key)
		if len(c.order) > queryEmbedCacheSize {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.m, oldest)
		}
	}
	c.m[key] = vec
}

// queryPrefixer is implemented by embedding providers whose models use an
// asymmetric query/document scheme and need an instruction prepended to
// query (never document) embeddings.
type queryPrefixer interface {
	QueryPrefix() string
}
