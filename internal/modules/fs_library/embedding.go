package fslibrary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// EmbedInput represents text to be embedded.
type EmbedInput struct {
	ID   string
	Text string
}

// EmbedVector represents an embedding result.
type EmbedVector struct {
	ID     string    `json:"id"`
	Vector []float32 `json:"vector"`
}

// EmbeddingProvider generates embeddings for text.
type EmbeddingProvider interface {
	Name() string
	Embed(ctx context.Context, inputs []EmbedInput) ([]EmbedVector, error)
	Dimension() int
}

// OllamaProvider implements EmbeddingProvider using a local Ollama server.
type OllamaProvider struct {
	endpoint  string
	model     string
	timeout   time.Duration
	batchSize int
	dimension int
	http      *http.Client
}

// OllamaConfig configures the Ollama embedding provider.
type OllamaConfig struct {
	Endpoint  string
	Model     string
	Timeout   time.Duration
	BatchSize int
}

// NewOllamaProvider creates an Ollama embedding provider.
func NewOllamaProvider(cfg OllamaConfig) (*OllamaProvider, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = "http://localhost:11434"
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "nomic-embed-text"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}

	return &OllamaProvider{
		endpoint:  strings.TrimRight(cfg.Endpoint, "/"),
		model:     cfg.Model,
		timeout:   cfg.Timeout,
		batchSize: cfg.BatchSize,
		dimension: 768, // Default for nomic-embed-text, updated on first embed
		http: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxConnsPerHost:     4,
				MaxIdleConns:        8,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}, nil
}

func (p *OllamaProvider) Name() string {
	return "ollama:" + p.model
}

func (p *OllamaProvider) Dimension() int {
	return p.dimension
}

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (p *OllamaProvider) Embed(ctx context.Context, inputs []EmbedInput) ([]EmbedVector, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	results := make([]EmbedVector, len(inputs))
	var wg sync.WaitGroup
	errChan := make(chan error, len(inputs))
	sem := make(chan struct{}, p.batchSize)

	for i, input := range inputs {
		wg.Add(1)
		go func(idx int, in EmbedInput) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			vec, err := p.embedOne(ctx, in.Text)
			if err != nil {
				errChan <- fmt.Errorf("embed %s: %w", in.ID, err)
				return
			}
			results[idx] = EmbedVector{ID: in.ID, Vector: vec}
		}(i, input)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return results, errors.Join(errs...)
	}

	// Update dimension from first result
	if len(results) > 0 && len(results[0].Vector) > 0 {
		p.dimension = len(results[0].Vector)
	}

	return results, nil
}

func (p *OllamaProvider) embedOne(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model: p.model,
		Input: text,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10*1024*1024)).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings")
	}

	return result.Embeddings[0], nil
}

// OllamaGenerator calls Ollama's /api/generate endpoint to produce text.
type OllamaGenerator struct {
	endpoint string
	model    string
	http     *http.Client
}

// NewOllamaGenerator creates an OllamaGenerator.
func NewOllamaGenerator(endpoint, model string) *OllamaGenerator {
	return &OllamaGenerator{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		http: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxConnsPerHost:     2,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

// Generate sends a prompt to Ollama and returns the generated text.
func (g *OllamaGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	reqBody := ollamaGenerateRequest{
		Model:  g.model,
		Prompt: prompt,
		Stream: false,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", g.endpoint+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ollama generate error %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaGenerateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&result); err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Response), nil
}

// EmbeddingCache provides persistent caching for embeddings.
type EmbeddingCache struct {
	dir string
	mu  sync.RWMutex
	mem map[string][]float32
}

// NewEmbeddingCache creates an embedding cache.
func NewEmbeddingCache(dir string) (*EmbeddingCache, error) {
	if dir == "" {
		return &EmbeddingCache{mem: make(map[string][]float32)}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &EmbeddingCache{
		dir: dir,
		mem: make(map[string][]float32),
	}, nil
}

func (c *EmbeddingCache) cacheKey(itemID, text string) string {
	h := sha256.New()
	h.Write([]byte(itemID))
	h.Write([]byte("|"))
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (c *EmbeddingCache) Get(itemID, text string) ([]float32, bool) {
	key := c.cacheKey(itemID, text)

	c.mu.RLock()
	if vec, ok := c.mem[key]; ok {
		c.mu.RUnlock()
		return vec, true
	}
	c.mu.RUnlock()

	if c.dir == "" {
		return nil, false
	}

	path := filepath.Join(c.dir, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var vec []float32
	if err := json.Unmarshal(data, &vec); err != nil {
		return nil, false
	}

	c.mu.Lock()
	c.mem[key] = vec
	c.mu.Unlock()

	return vec, true
}

func (c *EmbeddingCache) Put(itemID, text string, vec []float32) {
	key := c.cacheKey(itemID, text)

	c.mu.Lock()
	c.mem[key] = vec
	c.mu.Unlock()

	if c.dir == "" {
		return
	}

	data, err := json.Marshal(vec)
	if err != nil {
		return
	}
	path := filepath.Join(c.dir, key+".json")
	_ = os.WriteFile(path, data, 0o640)
}

// VectorIndex stores item embeddings for similarity search.
type VectorIndex struct {
	mu      sync.RWMutex
	vectors map[string][]float32
}

// NewVectorIndex creates a vector index.
func NewVectorIndex() *VectorIndex {
	return &VectorIndex{
		vectors: make(map[string][]float32),
	}
}

func (idx *VectorIndex) Add(id string, vec []float32) {
	idx.mu.Lock()
	idx.vectors[id] = vec
	idx.mu.Unlock()
}

func (idx *VectorIndex) Remove(id string) {
	idx.mu.Lock()
	delete(idx.vectors, id)
	idx.mu.Unlock()
}

func (idx *VectorIndex) Clear() {
	idx.mu.Lock()
	idx.vectors = make(map[string][]float32)
	idx.mu.Unlock()
}

// Size returns the number of vectors in the index.
func (idx *VectorIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.vectors)
}

// SimilarityResult represents a similarity search result.
type SimilarityResult struct {
	ID    string
	Score float32
}

// Search finds the most similar items to the query vector.
func (idx *VectorIndex) Search(query []float32, limit int) []SimilarityResult {
	if len(query) == 0 || limit <= 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	results := make([]SimilarityResult, 0, len(idx.vectors))
	for id, vec := range idx.vectors {
		score := cosineSimilarity(query, vec)
		results = append(results, SimilarityResult{ID: id, Score: score})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

const (
	vectorSuffixCard    = ":card"
	vectorSuffixSummary = ":summary"
)

// SearchDual performs weighted dual-vector search, combining card and summary vectors.
// Keys ending in :card are treated as primary; the corresponding :summary key is looked up.
// If no summary vector exists for an item, the card score is used at full weight (no penalty).
// Returns results with base item IDs (suffixes stripped).
func (idx *VectorIndex) SearchDual(query []float32, cardWeight, summaryWeight float32, limit int) []SimilarityResult {
	if len(query) == 0 || limit <= 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Find all card vectors and compute combined scores.
	type scored struct {
		baseID string
		score  float32
	}
	var items []scored

	for id, vec := range idx.vectors {
		if !strings.HasSuffix(id, vectorSuffixCard) {
			continue
		}
		baseID := strings.TrimSuffix(id, vectorSuffixCard)
		cardScore := cosineSimilarity(query, vec)

		var total float32
		if summaryVec, ok := idx.vectors[baseID+vectorSuffixSummary]; ok {
			summaryScore := cosineSimilarity(query, summaryVec)
			total = cardWeight*cardScore + summaryWeight*summaryScore
		} else {
			// No summary vector — use card score at full weight.
			total = cardScore
		}

		items = append(items, scored{baseID: baseID, score: total})
	}

	if len(items) == 0 {
		return nil
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	if len(items) > limit {
		items = items[:limit]
	}

	results := make([]SimilarityResult, len(items))
	for i, it := range items {
		results[i] = SimilarityResult{ID: it.baseID, Score: it.score}
	}
	return results
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	// Stay in float32 to avoid per-element float64 conversions.
	// Only widen to float64 for the final sqrt to avoid precision loss there.
	var dot, normA, normB float32
	for i := range a {
		ai, bi := a[i], b[i]
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(float64(dot) / (math.Sqrt(float64(normA)) * math.Sqrt(float64(normB))))
}

// genreSynonyms maps common genre/style spelling variants to a canonical form.
var genreSynonyms = map[string]string{
	"rock & roll":   "rock and roll",
	"rock 'n' roll": "rock and roll",
	"r&b":           "rhythm and blues",
	"r & b":         "rhythm and blues",
	"hip hop":       "hip-hop",
	"synth pop":     "synthpop",
	"drum and bass": "drum n bass",
	"post punk":     "post-punk",
	"nu metal":      "nu-metal",
	"lo fi":         "lo-fi",
}

// normalizeStringList lowercases, trims, applies synonyms, deduplicates, and sorts a list of strings.
func normalizeStringList(items []string, synonyms map[string]string) []string {
	seen := make(map[string]struct{}, len(items))
	var out []string
	for _, s := range items {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if synonyms != nil {
			if canon, ok := synonyms[s]; ok {
				s = canon
			}
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func normalizeGenres(genres []string) []string  { return normalizeStringList(genres, genreSynonyms) }
func normalizeTags(tags []string) []string       { return normalizeStringList(tags, nil) }
func normalizeStyles(styles []string) []string   { return normalizeStringList(styles, genreSynonyms) }

// writeLine appends "label: value\n" to the builder if value is non-empty.
func writeLine(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteByte('\n')
}

// collectGenres merges MusicBrainz album genres and artist genres, normalizes the result.
func collectGenres(enrich *AlbumMetadata) []string {
	if enrich == nil {
		return nil
	}
	var raw []string
	if mb := enrich.MusicBrainz; mb != nil {
		raw = append(raw, mb.Genres...)
	}
	if ai := enrich.ArtistInfo; ai != nil {
		raw = append(raw, ai.Genres...)
	}
	return normalizeGenres(raw)
}

// collectTags merges MusicBrainz tags (top 5) and artist tags (top 5), normalizes the result.
func collectTags(enrich *AlbumMetadata) []string {
	if enrich == nil {
		return nil
	}
	var raw []string
	if mb := enrich.MusicBrainz; mb != nil {
		tags := mb.Tags
		if len(tags) > 5 {
			tags = tags[:5]
		}
		raw = append(raw, tags...)
	}
	if ai := enrich.ArtistInfo; ai != nil {
		tags := ai.Tags
		if len(tags) > 5 {
			tags = tags[:5]
		}
		raw = append(raw, tags...)
	}
	return normalizeTags(raw)
}

// uniqueNames extracts unique names from Discogs credits, capped at limit.
func uniqueNames(credits []DiscogsCredit, limit int) []string {
	seen := make(map[string]struct{}, limit)
	var names []string
	for _, c := range credits {
		if _, dup := seen[c.Name]; dup {
			continue
		}
		seen[c.Name] = struct{}{}
		names = append(names, c.Name)
		if len(names) >= limit {
			break
		}
	}
	return names
}

// buildAlbumContext returns a one-liner like "Kind of Blue (1959, Columbia) -- cool jazz, modal jazz".
func buildAlbumContext(enrich *AlbumMetadata) string {
	if enrich == nil {
		return ""
	}
	album := enrich.Album
	if album == "" {
		return ""
	}

	var parts []string
	if mb := enrich.MusicBrainz; mb != nil && mb.Year > 0 {
		parts = append(parts, fmt.Sprintf("%d", mb.Year))
	}
	if mb := enrich.MusicBrainz; mb != nil && mb.Label != "" {
		parts = append(parts, mb.Label)
	}

	ctx := album
	if len(parts) > 0 {
		ctx += " (" + strings.Join(parts, ", ") + ")"
	}

	genres := collectGenres(enrich)
	if len(genres) > 2 {
		genres = genres[:2]
	}
	if len(genres) > 0 {
		ctx += " -- " + strings.Join(genres, ", ")
	}
	return ctx
}

// buildEmbedText creates structured labeled card text from a media item for embedding.
// If enrich is non-nil, genres, tags, year, styles, credits, and artist info are included.
func buildEmbedText(item mediaItem, enrich *AlbumMetadata) string {
	var b strings.Builder

	// Type
	switch item.MediaType {
	case "Audio":
		writeLine(&b, "type", "audio")
	case "Video":
		writeLine(&b, "type", "video")
	default:
		writeLine(&b, "type", strings.ToLower(item.MediaType))
	}

	// Core fields
	writeLine(&b, "title", item.Title)
	if len(item.Artists) > 0 {
		writeLine(&b, "artist", strings.Join(item.Artists, "; "))
	}
	writeLine(&b, "album", item.Album)

	if enrich != nil {
		// Year
		if mb := enrich.MusicBrainz; mb != nil && mb.Year > 0 {
			writeLine(&b, "year", fmt.Sprintf("%d", mb.Year))
		}

		// Genres (merged MB + artist, normalized)
		genres := collectGenres(enrich)
		if len(genres) > 0 {
			writeLine(&b, "genres", strings.Join(genres, "; "))
		}

		// Styles (Discogs, normalized)
		if dc := enrich.Discogs; dc != nil {
			styles := normalizeStyles(dc.Styles)
			if len(styles) > 0 {
				writeLine(&b, "styles", strings.Join(styles, "; "))
			}
		}

		// Instruments (Discogs)
		if dc := enrich.Discogs; dc != nil && len(dc.Instruments) > 0 {
			writeLine(&b, "instruments", strings.Join(dc.Instruments, "; "))
		}

		// Tags (merged MB + artist, normalized)
		tags := collectTags(enrich)
		if len(tags) > 0 {
			writeLine(&b, "tags", strings.Join(tags, "; "))
		}

		// Label
		if mb := enrich.MusicBrainz; mb != nil {
			writeLine(&b, "label", mb.Label)
			writeLine(&b, "recording_type", mb.ReleaseType)
		}

		// Personnel (Discogs credits, top 5)
		if dc := enrich.Discogs; dc != nil {
			personnel := uniqueNames(dc.Credits, 5)
			if len(personnel) > 0 {
				writeLine(&b, "personnel", strings.Join(personnel, "; "))
			}
			// Producers (Discogs release credits, top 3)
			producers := uniqueNames(dc.ReleaseCredits, 3)
			if len(producers) > 0 {
				writeLine(&b, "producers", strings.Join(producers, "; "))
			}
		}

		// Artist info
		if ai := enrich.ArtistInfo; ai != nil {
			writeLine(&b, "artist_type", ai.Type)
			writeLine(&b, "artist_origin", ai.Origin)
			writeLine(&b, "artist_active", ai.ActiveBegin)
			members := ai.Members
			if len(members) > 5 {
				members = members[:5]
			}
			if len(members) > 0 {
				writeLine(&b, "members", strings.Join(members, "; "))
			}
			if ai.Biography != "" {
				bio := ai.Biography
				if len(bio) > 200 {
					bio = bio[:200]
				}
				writeLine(&b, "biography", bio)
			}
		}

		// Album description
		if desc := enrich.Description; desc != nil {
			if desc.WikipediaSummary != "" {
				summary := desc.WikipediaSummary
				if len(summary) > 200 {
					summary = summary[:200]
				}
				writeLine(&b, "description", summary)
			}
		}

		// Album context (audio/track types only)
		if item.MediaType == "Audio" {
			ctx := buildAlbumContext(enrich)
			writeLine(&b, "album_context", ctx)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// buildSummaryText creates summary-oriented embedding text from existing sidecar data.
// Returns "" if no WikipediaSummary and no MBAnnotation exist — no summary vector should be generated.
func buildSummaryText(item mediaItem, enrich *AlbumMetadata) string {
	if enrich == nil {
		return ""
	}

	// Determine summary text from available sources.
	var summaryText string
	if desc := enrich.Description; desc != nil {
		if desc.GeneratedSummary != "" {
			summaryText = desc.GeneratedSummary
		} else if desc.WikipediaSummary != "" {
			summaryText = desc.WikipediaSummary
		} else if desc.MBAnnotation != "" {
			summaryText = desc.MBAnnotation
		}
	}
	if summaryText == "" {
		if mb := enrich.MusicBrainz; mb != nil && mb.Annotation != "" {
			summaryText = mb.Annotation
		}
	}
	if summaryText == "" {
		return ""
	}

	if len(summaryText) > 200 {
		summaryText = summaryText[:200]
	}

	var b strings.Builder

	// Type
	switch item.MediaType {
	case "Audio":
		writeLine(&b, "type", "audio summary")
	case "Video":
		writeLine(&b, "type", "video summary")
	default:
		writeLine(&b, "type", strings.ToLower(item.MediaType)+" summary")
	}

	writeLine(&b, "title", item.Title)
	if len(item.Artists) > 0 {
		writeLine(&b, "artist", strings.Join(item.Artists, "; "))
	}
	writeLine(&b, "album", item.Album)

	if mb := enrich.MusicBrainz; mb != nil && mb.Year > 0 {
		writeLine(&b, "year", fmt.Sprintf("%d", mb.Year))
	}

	writeLine(&b, "summary", summaryText)

	// Keywords = merged genres + styles + instruments + tags (normalized, deduped)
	var kwRaw []string
	genres := collectGenres(enrich)
	kwRaw = append(kwRaw, genres...)
	if dc := enrich.Discogs; dc != nil {
		kwRaw = append(kwRaw, dc.Instruments...)
		styles := normalizeStyles(dc.Styles)
		kwRaw = append(kwRaw, styles...)
	}
	tags := collectTags(enrich)
	kwRaw = append(kwRaw, tags...)
	keywords := normalizeStringList(kwRaw, nil)
	if len(keywords) > 0 {
		writeLine(&b, "keywords", strings.Join(keywords, "; "))
	}

	return strings.TrimRight(b.String(), "\n")
}
