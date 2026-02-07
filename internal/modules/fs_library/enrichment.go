package fslibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const sidecarFileName = ".mu_album_metadata.json"

// AlbumMetadata is the enrichment sidecar schema.
type AlbumMetadata struct {
	Version     int              `json:"version"`
	FetchedAt   time.Time        `json:"fetched_at"`
	Artist      string           `json:"artist"`
	Album       string           `json:"album"`
	MusicBrainz *MBMetadata      `json:"musicbrainz"`
	Discogs     *DiscogsMetadata `json:"discogs"`
}

// MBMetadata holds data fetched from MusicBrainz.
type MBMetadata struct {
	ReleaseGroupID string   `json:"release_group_id,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Year           int      `json:"year,omitempty"`
	ReleaseType    string   `json:"release_type,omitempty"`
	Label          string   `json:"label,omitempty"`
}

// DiscogsMetadata holds data fetched from Discogs.
type DiscogsMetadata struct {
	MasterID    int             `json:"master_id,omitempty"`
	Styles      []string        `json:"styles,omitempty"`
	Credits     []DiscogsCredit `json:"credits,omitempty"`
	Notes       string          `json:"notes,omitempty"`
	LabelDetail string          `json:"label_detail,omitempty"`
}

// DiscogsCredit represents a personnel credit from Discogs.
type DiscogsCredit struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// enrichTarget identifies an album to enrich.
type enrichTarget struct {
	Artist string
	Album  string
	Dir    string
}

// rateLimiter provides simple rate limiting via a ticker.
type rateLimiter struct {
	ticker *time.Ticker
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	return &rateLimiter{ticker: time.NewTicker(interval)}
}

func (r *rateLimiter) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.ticker.C:
		return nil
	}
}

func (r *rateLimiter) Stop() {
	r.ticker.Stop()
}

// mbClient queries the MusicBrainz API.
type mbClient struct {
	http    *http.Client
	limiter *rateLimiter
}

func newMBClient() *mbClient {
	return &mbClient{
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxConnsPerHost:     2,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     60 * time.Second,
			},
		},
		limiter: newRateLimiter(1100 * time.Millisecond),
	}
}

func (c *mbClient) Close() { c.limiter.Stop() }

// mbSearchResponse is the MusicBrainz release-group search response.
type mbSearchResponse struct {
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbReleaseGroup struct {
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	PrimaryType    string      `json:"primary-type"`
	Score          int         `json:"score"`
	Genres         []mbGenre   `json:"genres"`
	Tags           []mbTag     `json:"tags"`
	Releases       []mbRelease `json:"releases"`
	FirstRelease   string      `json:"first-release-date"`
}

type mbGenre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type mbTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type mbRelease struct {
	Date       string         `json:"date"`
	LabelInfo  []mbLabelInfo  `json:"label-info"`
}

type mbLabelInfo struct {
	Label mbLabel `json:"label"`
}

type mbLabel struct {
	Name string `json:"name"`
}

func (c *mbClient) searchRelease(ctx context.Context, artist, album string) (*MBMetadata, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	query := fmt.Sprintf("artist:\"%s\" AND releasegroup:\"%s\"", artist, album)
	u := fmt.Sprintf("https://musicbrainz.org/ws/2/release-group/?query=%s&fmt=json&limit=5",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MediaUtopia/1.0 (https://github.com/mikey-austin/media_utopia)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz search: status %d", resp.StatusCode)
	}

	var searchResp mbSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("musicbrainz search decode: %w", err)
	}

	if len(searchResp.ReleaseGroups) == 0 {
		return nil, nil
	}

	// Pick best match by score
	best := searchResp.ReleaseGroups[0]
	for _, rg := range searchResp.ReleaseGroups[1:] {
		if rg.Score > best.Score {
			best = rg
		}
	}

	// Fetch full details with tags, genres, releases
	return c.fetchReleaseGroup(ctx, best.ID)
}

func (c *mbClient) fetchReleaseGroup(ctx context.Context, id string) (*MBMetadata, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("https://musicbrainz.org/ws/2/release-group/%s?inc=tags+genres+releases&fmt=json", url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MediaUtopia/1.0 (https://github.com/mikey-austin/media_utopia)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz release-group: status %d", resp.StatusCode)
	}

	var rg mbReleaseGroup
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&rg); err != nil {
		return nil, fmt.Errorf("musicbrainz release-group decode: %w", err)
	}

	meta := &MBMetadata{
		ReleaseGroupID: rg.ID,
		ReleaseType:    rg.PrimaryType,
	}

	for _, g := range rg.Genres {
		if g.Name != "" {
			meta.Genres = append(meta.Genres, g.Name)
		}
	}
	for _, t := range rg.Tags {
		if t.Name != "" {
			meta.Tags = append(meta.Tags, t.Name)
		}
	}

	// Extract year from first-release-date or earliest release
	if y := parseYear(rg.FirstRelease); y > 0 {
		meta.Year = y
	} else {
		for _, rel := range rg.Releases {
			if y := parseYear(rel.Date); y > 0 {
				if meta.Year == 0 || y < meta.Year {
					meta.Year = y
				}
			}
		}
	}

	// Extract label from first release with label info
	for _, rel := range rg.Releases {
		for _, li := range rel.LabelInfo {
			if li.Label.Name != "" {
				meta.Label = li.Label.Name
				break
			}
		}
		if meta.Label != "" {
			break
		}
	}

	return meta, nil
}

func (c *mbClient) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfter := resp.Header.Get("Retry-After")
		wait := 2 * time.Second
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 && secs <= 60 {
			wait = time.Duration(secs) * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		return c.http.Do(req)
	}
	return resp, nil
}

// discogsClient queries the Discogs API.
type discogsClient struct {
	http    *http.Client
	limiter *rateLimiter
	token   string
}

func newDiscogsClient(token string) *discogsClient {
	interval := 2500 * time.Millisecond
	if token != "" {
		interval = 1100 * time.Millisecond
	}
	return &discogsClient{
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxConnsPerHost:     2,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     60 * time.Second,
			},
		},
		limiter: newRateLimiter(interval),
		token:   token,
	}
}

func (c *discogsClient) Close() { c.limiter.Stop() }

type discogsSearchResponse struct {
	Results []discogsSearchResult `json:"results"`
}

type discogsSearchResult struct {
	ID       int    `json:"id"`
	MasterID int    `json:"master_id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
}

type discogsMasterResponse struct {
	ID      int              `json:"id"`
	Styles  []string         `json:"styles"`
	Notes   string           `json:"notes"`
	Artists []discogsArtist  `json:"artists"`
	Labels  []discogsLabel   `json:"labels"`
	Tracklist []discogsTrack `json:"tracklist"`
}

type discogsArtist struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type discogsLabel struct {
	Name  string `json:"name"`
	Catno string `json:"catno"`
}

type discogsTrack struct {
	ExtraArtists []discogsExtraArtist `json:"extraartists"`
}

type discogsExtraArtist struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

func (c *discogsClient) setAuth(req *http.Request) {
	req.Header.Set("User-Agent", "MediaUtopia/1.0")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Discogs token="+c.token)
	}
}

func (c *discogsClient) searchRelease(ctx context.Context, artist, album string) (*DiscogsMetadata, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("artist", artist)
	params.Set("release_title", album)
	params.Set("type", "master")
	u := "https://api.discogs.com/database/search?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discogs search: status %d", resp.StatusCode)
	}

	var searchResp discogsSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("discogs search decode: %w", err)
	}

	// Find the best master result
	var masterID int
	for _, r := range searchResp.Results {
		if r.MasterID > 0 {
			masterID = r.MasterID
			break
		}
		if r.Type == "master" && r.ID > 0 {
			masterID = r.ID
			break
		}
	}
	if masterID == 0 {
		return nil, nil
	}

	return c.fetchMaster(ctx, masterID)
}

func (c *discogsClient) fetchMaster(ctx context.Context, masterID int) (*DiscogsMetadata, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("https://api.discogs.com/masters/%d", masterID)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discogs master: status %d", resp.StatusCode)
	}

	var master discogsMasterResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&master); err != nil {
		return nil, fmt.Errorf("discogs master decode: %w", err)
	}

	meta := &DiscogsMetadata{
		MasterID: master.ID,
		Styles:   master.Styles,
		Notes:    master.Notes,
	}

	// Extract credits from tracklist extra artists
	seen := map[string]bool{}
	for _, track := range master.Tracklist {
		for _, ea := range track.ExtraArtists {
			key := ea.Name + "|" + ea.Role
			if !seen[key] && ea.Name != "" {
				seen[key] = true
				meta.Credits = append(meta.Credits, DiscogsCredit{
					Name: ea.Name,
					Role: ea.Role,
				})
			}
		}
	}

	// Build label detail
	if len(master.Labels) > 0 {
		var parts []string
		for _, l := range master.Labels {
			s := l.Name
			if l.Catno != "" {
				s += ", " + l.Catno
			}
			parts = append(parts, s)
		}
		meta.LabelDetail = strings.Join(parts, "; ")
	}

	return meta, nil
}

func (c *discogsClient) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfter := resp.Header.Get("Retry-After")
		wait := 3 * time.Second
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 && secs <= 60 {
			wait = time.Duration(secs) * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		return c.http.Do(req)
	}
	return resp, nil
}

// Sidecar I/O

func sidecarPath(dir string) string {
	return filepath.Join(dir, sidecarFileName)
}

func readSidecar(dir string) (*AlbumMetadata, error) {
	data, err := os.ReadFile(sidecarPath(dir))
	if err != nil {
		return nil, err
	}
	var meta AlbumMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func writeSidecar(dir string, meta *AlbumMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sidecarPath(dir), data, 0o640)
}

func sidecarExists(dir string) bool {
	_, err := os.Stat(sidecarPath(dir))
	return err == nil
}

// sidecarNeedsRefresh returns true if the sidecar is a negative cache entry older than 30 days.
func sidecarNeedsRefresh(meta *AlbumMetadata) bool {
	if meta.MusicBrainz != nil || meta.Discogs != nil {
		return false
	}
	return time.Since(meta.FetchedAt) > 30*24*time.Hour
}

// enrichAlbums queries MusicBrainz and Discogs for each target, writes sidecars,
// and rebuilds embeddings if any albums were enriched.
func (m *Module) enrichAlbums(ctx context.Context, targets []enrichTarget) {
	m.log.Info("enrichment starting", zap.Int("albums", len(targets)))

	mb := newMBClient()
	defer mb.Close()

	dc := newDiscogsClient(m.config.DiscogsToken)
	defer dc.Close()

	enriched := 0
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}

		meta := &AlbumMetadata{
			Version:   1,
			FetchedAt: time.Now().UTC(),
			Artist:    t.Artist,
			Album:     t.Album,
		}

		// Query MusicBrainz
		mbMeta, err := mb.searchRelease(ctx, t.Artist, t.Album)
		if err != nil {
			m.log.Debug("musicbrainz query failed",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album),
				zap.Error(err))
		} else {
			meta.MusicBrainz = mbMeta
		}

		// Query Discogs
		dcMeta, err := dc.searchRelease(ctx, t.Artist, t.Album)
		if err != nil {
			m.log.Debug("discogs query failed",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album),
				zap.Error(err))
		} else {
			meta.Discogs = dcMeta
		}

		// Write sidecar (even if both nil, as negative cache)
		if err := writeSidecar(t.Dir, meta); err != nil {
			m.log.Warn("failed to write sidecar",
				zap.String("dir", t.Dir),
				zap.Error(err))
			continue
		}

		// Update in-memory enrichment map
		key := t.Artist + "|" + t.Album
		m.mu.Lock()
		m.enrichMeta[key] = meta
		m.mu.Unlock()

		if meta.MusicBrainz != nil || meta.Discogs != nil {
			enriched++
			m.log.Debug("album enriched",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album))
		}
	}

	m.log.Info("enrichment complete",
		zap.Int("enriched", enriched),
		zap.Int("total", len(targets)))

	// Rebuild embeddings if any albums were enriched
	if enriched > 0 {
		m.mu.RLock()
		items := m.index.Items
		m.mu.RUnlock()
		m.buildEmbeddings(items)
	}
}

func parseYear(dateStr string) int {
	dateStr = strings.TrimSpace(dateStr)
	if len(dateStr) < 4 {
		return 0
	}
	y, err := strconv.Atoi(dateStr[:4])
	if err != nil {
		return 0
	}
	if y < 1900 || y > 2100 {
		return 0
	}
	return y
}
