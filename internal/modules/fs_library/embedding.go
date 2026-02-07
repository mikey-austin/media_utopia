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
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
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
		Model:  p.model,
		Prompt: text,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/embeddings", bytes.NewReader(payload))
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

	return result.Embedding, nil
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
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// buildEmbedText creates searchable text from a media item.
func buildEmbedText(item mediaItem) string {
	parts := []string{}
	if item.Title != "" {
		parts = append(parts, item.Title)
	}
	if len(item.Artists) > 0 {
		parts = append(parts, strings.Join(item.Artists, ", "))
	}
	if item.Album != "" {
		parts = append(parts, item.Album)
	}
	if item.Name != "" && item.Name != item.Title {
		parts = append(parts, item.Name)
	}
	return strings.Join(parts, " - ")
}
