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
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const sidecarFileName = ".mu_album_metadata.json"
const currentSidecarVersion = 3

// AlbumMetadata is the enrichment sidecar schema.
type AlbumMetadata struct {
	Version     int               `json:"version"`
	FetchedAt   time.Time         `json:"fetched_at"`
	Artist      string            `json:"artist"`
	Album       string            `json:"album"`
	MusicBrainz *MBMetadata       `json:"musicbrainz"`
	Discogs     *DiscogsMetadata  `json:"discogs"`
	ArtistInfo  *ArtistInfo       `json:"artist_info,omitempty"`
	Description *AlbumDescription `json:"description,omitempty"`
}

// ArtistInfo holds enriched artist metadata from MusicBrainz and Discogs.
type ArtistInfo struct {
	Name           string   `json:"name"`
	Type           string   `json:"type,omitempty"`
	Origin         string   `json:"origin,omitempty"`
	ActiveBegin    string   `json:"active_begin,omitempty"`
	ActiveEnd      string   `json:"active_end,omitempty"`
	Disambiguation string   `json:"disambiguation,omitempty"`
	Biography      string   `json:"biography,omitempty"`
	Members        []string `json:"members,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

// AlbumDescription holds album-level descriptive text.
type AlbumDescription struct {
	MBAnnotation     string `json:"mb_annotation,omitempty"`
	WikipediaSummary string `json:"wikipedia_summary,omitempty"`
	GeneratedSummary string `json:"generated_summary,omitempty"`
}

// MBMetadata holds data fetched from MusicBrainz.
type MBMetadata struct {
	ReleaseGroupID string   `json:"release_group_id,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Year           int      `json:"year,omitempty"`
	ReleaseType    string   `json:"release_type,omitempty"`
	Label          string   `json:"label,omitempty"`
	Annotation     string   `json:"annotation,omitempty"`
	WikipediaURL   string   `json:"wikipedia_url,omitempty"`
	ArtistIDs      []string `json:"artist_ids,omitempty"`
}

// DiscogsMetadata holds data fetched from Discogs.
type DiscogsMetadata struct {
	MasterID       int             `json:"master_id,omitempty"`
	Styles         []string        `json:"styles,omitempty"`
	Credits        []DiscogsCredit `json:"credits,omitempty"`
	Notes          string          `json:"notes,omitempty"`
	LabelDetail    string          `json:"label_detail,omitempty"`
	MainReleaseID  int             `json:"main_release_id,omitempty"`
	ReleaseNotes   string          `json:"release_notes,omitempty"`
	ReleaseCredits []DiscogsCredit `json:"release_credits,omitempty"`
	ArtistID       int             `json:"artist_id,omitempty"`
	Instruments    []string        `json:"instruments,omitempty"`
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
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	PrimaryType    string           `json:"primary-type"`
	Score          int              `json:"score"`
	Genres         []mbGenre        `json:"genres"`
	Tags           []mbTag          `json:"tags"`
	Releases       []mbRelease      `json:"releases"`
	FirstRelease   string           `json:"first-release-date"`
	Annotation     string           `json:"annotation"`
	Relations      []mbRelation     `json:"relations"`
	ArtistCredit   []mbArtistCredit `json:"artist-credit"`
}

type mbRelation struct {
	Type string   `json:"type"`
	URL  mbRelURL `json:"url"`
}

type mbRelURL struct {
	Resource string `json:"resource"`
}

type mbArtistCredit struct {
	Artist mbArtistRef `json:"artist"`
}

type mbArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mbArtistResponse struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Type           string       `json:"type"`
	Disambiguation string       `json:"disambiguation"`
	Area           mbArea       `json:"area"`
	LifeSpan       mbLifeSpan   `json:"life-span"`
	Genres         []mbGenre    `json:"genres"`
	Tags           []mbTag      `json:"tags"`
	Relations      []mbRelation `json:"relations"`
}

type mbArea struct {
	Name string `json:"name"`
}

type mbLifeSpan struct {
	Begin string `json:"begin"`
	End   string `json:"end"`
	Ended bool   `json:"ended"`
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

	u := fmt.Sprintf("https://musicbrainz.org/ws/2/release-group/%s?inc=tags+genres+releases+annotation+url-rels+artist-credits&fmt=json", url.PathEscape(id))
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

	// Extract annotation (cap at 2000 chars)
	if rg.Annotation != "" {
		ann := rg.Annotation
		if len(ann) > 2000 {
			ann = ann[:2000]
		}
		meta.Annotation = ann
	}

	// Extract Wikipedia URL from relations
	for _, rel := range rg.Relations {
		if rel.Type == "wikipedia" && rel.URL.Resource != "" {
			meta.WikipediaURL = rel.URL.Resource
			break
		}
	}

	// Extract artist IDs from artist credits
	for _, ac := range rg.ArtistCredit {
		if ac.Artist.ID != "" {
			meta.ArtistIDs = append(meta.ArtistIDs, ac.Artist.ID)
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
	ID          int              `json:"id"`
	MainRelease int              `json:"main_release"`
	Styles      []string         `json:"styles"`
	Notes       string           `json:"notes"`
	Artists     []discogsArtist  `json:"artists"`
	Labels      []discogsLabel   `json:"labels"`
	Tracklist   []discogsTrack   `json:"tracklist"`
}

type discogsArtist struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type discogsReleaseResponse struct {
	ID           int                 `json:"id"`
	Notes        string              `json:"notes"`
	ExtraArtists []discogsExtraArtist `json:"extraartists"`
}

type discogsArtistResponse struct {
	ID       int             `json:"id"`
	Name     string          `json:"name"`
	RealName string          `json:"realname"`
	Profile  string          `json:"profile"`
	Members  []discogsMember `json:"members"`
}

type discogsMember struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type wikipediaSummary struct {
	Title   string `json:"title"`
	Extract string `json:"extract"`
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
		MasterID:      master.ID,
		MainReleaseID: master.MainRelease,
		Styles:        master.Styles,
		Notes:         master.Notes,
	}

	// Capture first artist ID
	if len(master.Artists) > 0 && master.Artists[0].ID > 0 {
		meta.ArtistID = master.Artists[0].ID
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

// fetchArtist fetches artist details from MusicBrainz.
func (c *mbClient) fetchArtist(ctx context.Context, artistID string) (*ArtistInfo, string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, "", err
	}

	u := fmt.Sprintf("https://musicbrainz.org/ws/2/artist/%s?inc=tags+genres+url-rels&fmt=json", url.PathEscape(artistID))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "MediaUtopia/1.0 (https://github.com/mikey-austin/media_utopia)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("musicbrainz artist: status %d", resp.StatusCode)
	}

	var ar mbArtistResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&ar); err != nil {
		return nil, "", fmt.Errorf("musicbrainz artist decode: %w", err)
	}

	info := &ArtistInfo{
		Name:           ar.Name,
		Type:           ar.Type,
		Origin:         ar.Area.Name,
		ActiveBegin:    ar.LifeSpan.Begin,
		ActiveEnd:      ar.LifeSpan.End,
		Disambiguation: ar.Disambiguation,
	}

	// Top 10 genres
	for i, g := range ar.Genres {
		if i >= 10 || g.Name == "" {
			break
		}
		info.Genres = append(info.Genres, g.Name)
	}

	// Top 10 tags
	for i, t := range ar.Tags {
		if i >= 10 || t.Name == "" {
			break
		}
		info.Tags = append(info.Tags, t.Name)
	}

	// Extract Wikipedia URL from relations
	var wikiURL string
	for _, rel := range ar.Relations {
		if rel.Type == "wikipedia" && rel.URL.Resource != "" {
			wikiURL = rel.URL.Resource
			break
		}
	}

	return info, wikiURL, nil
}

// fetchRelease fetches a Discogs release for fuller notes and credits.
func (c *discogsClient) fetchRelease(ctx context.Context, releaseID int) (string, []DiscogsCredit, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", nil, err
	}

	u := fmt.Sprintf("https://api.discogs.com/releases/%d", releaseID)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", nil, err
	}
	c.setAuth(req)

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("discogs release: status %d", resp.StatusCode)
	}

	var release discogsReleaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&release); err != nil {
		return "", nil, fmt.Errorf("discogs release decode: %w", err)
	}

	var credits []DiscogsCredit
	seen := map[string]bool{}
	for _, ea := range release.ExtraArtists {
		key := ea.Name + "|" + ea.Role
		if !seen[key] && ea.Name != "" {
			seen[key] = true
			credits = append(credits, DiscogsCredit{Name: ea.Name, Role: ea.Role})
		}
	}

	return release.Notes, credits, nil
}

// fetchArtist fetches artist details from Discogs.
func (c *discogsClient) fetchArtist(ctx context.Context, artistID int) (*discogsArtistResponse, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("https://api.discogs.com/artists/%d", artistID)
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
		return nil, fmt.Errorf("discogs artist: status %d", resp.StatusCode)
	}

	var artist discogsArtistResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&artist); err != nil {
		return nil, fmt.Errorf("discogs artist decode: %w", err)
	}

	return &artist, nil
}

// fetchWikipediaSummary fetches a page summary from Wikipedia.
func fetchWikipediaSummary(ctx context.Context, client *http.Client, wikiURL string) (string, error) {
	parsed, err := url.Parse(wikiURL)
	if err != nil {
		return "", err
	}
	// Extract page title from URL path (last segment)
	title := strings.TrimPrefix(parsed.Path, "/wiki/")
	if title == "" {
		return "", fmt.Errorf("no wiki title in URL: %s", wikiURL)
	}

	u := fmt.Sprintf("https://en.wikipedia.org/api/rest_v1/page/summary/%s", url.PathEscape(title))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MediaUtopia/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wikipedia summary: status %d", resp.StatusCode)
	}

	var summary wikipediaSummary
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&summary); err != nil {
		return "", fmt.Errorf("wikipedia summary decode: %w", err)
	}

	extract := summary.Extract
	if len(extract) > 1000 {
		extract = extract[:1000]
	}
	return extract, nil
}

// nonInstrumentRoles lists Discogs credit roles that are NOT instruments.
var nonInstrumentRoles = map[string]bool{
	"producer": true, "executive producer": true, "co-producer": true,
	"engineer": true, "recording engineer": true, "mixing engineer": true, "mastering engineer": true,
	"mixed by": true, "mastered by": true, "recorded by": true,
	"written by": true, "composed by": true, "arranged by": true,
	"conductor": true, "director": true, "art direction": true,
	"photography": true, "design": true, "liner notes": true,
	"remix": true, "remaster": true, "lacquer cut by": true,
	"a&r": true, "management": true, "other": true,
}

// extractInstruments collects unique instrument names from Discogs tracklist credits.
// Compound roles (e.g. "guitar, vocals") are split on comma. Non-instrument roles
// (producer, engineer, etc.) are filtered out. Results are deduped, sorted, and capped at 15.
func extractInstruments(credits []DiscogsCredit) []string {
	seen := make(map[string]struct{})
	var instruments []string
	for _, c := range credits {
		parts := strings.Split(c.Role, ",")
		for _, part := range parts {
			role := strings.ToLower(strings.TrimSpace(part))
			if role == "" {
				continue
			}
			if nonInstrumentRoles[role] {
				continue
			}
			if _, dup := seen[role]; dup {
				continue
			}
			seen[role] = struct{}{}
			instruments = append(instruments, role)
		}
	}
	sort.Strings(instruments)
	if len(instruments) > 15 {
		instruments = instruments[:15]
	}
	return instruments
}

// generateAlbumSummary uses the OllamaGenerator to produce a concise search-optimized
// album summary. Returns "" if the generator is nil or if generation fails.
func generateAlbumSummary(ctx context.Context, gen *OllamaGenerator, meta *AlbumMetadata) (string, error) {
	if gen == nil {
		return "", nil
	}

	var genres, styles, tags, personnel []string
	var year int
	var label, releaseType string

	if mb := meta.MusicBrainz; mb != nil {
		genres = append(genres, mb.Genres...)
		tags = mb.Tags
		year = mb.Year
		label = mb.Label
		releaseType = mb.ReleaseType
	}
	if ai := meta.ArtistInfo; ai != nil {
		genres = append(genres, ai.Genres...)
	}
	if dc := meta.Discogs; dc != nil {
		styles = dc.Styles
		names := uniqueNames(dc.Credits, 5)
		personnel = names
	}

	genresStr := strings.Join(genres, ", ")
	stylesStr := strings.Join(styles, ", ")
	tagsStr := strings.Join(tags, ", ")
	personnelStr := strings.Join(personnel, ", ")

	yearStr := ""
	if year > 0 {
		yearStr = fmt.Sprintf("%d", year)
	}

	prompt := fmt.Sprintf(`You write concise album summaries for semantic search.

Rules:
- Output 1-3 sentences.
- 45-70 words total.
- Describe overall sound and mood, instrumentation tendencies, and genre/era anchors.
- No hype words, no quotes, no bullet points.

Album:
Title: %s
Artist: %s
Year: %s
Genres: %s
Styles: %s
Label: %s
Recording type: %s
Tags: %s
Personnel: %s

Summary:`, meta.Album, meta.Artist, yearStr, genresStr, stylesStr, label, releaseType, tagsStr, personnelStr)

	return gen.Generate(ctx, prompt)
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

// sidecarNeedsRefresh returns true if the sidecar version is outdated or
// it is a negative cache entry older than 30 days.
func sidecarNeedsRefresh(meta *AlbumMetadata) bool {
	if meta.Version < currentSidecarVersion {
		return true
	}
	if meta.MusicBrainz == nil && meta.Discogs == nil {
		return time.Since(meta.FetchedAt) > 30*24*time.Hour
	}
	return false
}

// artistCacheEntry caches artist data within an enrichment run.
type artistCacheEntry struct {
	Info    *ArtistInfo
	WikiURL string
}

// enrichAlbums queries MusicBrainz and Discogs for each target, writes sidecars,
// and rebuilds embeddings if any albums were enriched.
func (m *Module) enrichAlbums(ctx context.Context, targets []enrichTarget) {
	m.log.Info("enrichment starting",
		zap.Int("albums", len(targets)),
		zap.Bool("summary_gen_available", m.summaryGen != nil))

	mb := newMBClient()
	defer mb.Close()

	dc := newDiscogsClient(m.config.DiscogsToken)
	defer dc.Close()

	// Artist caches to avoid re-fetching for artists with multiple albums
	mbArtistCache := map[string]*artistCacheEntry{}
	dcArtistCache := map[int]*artistCacheEntry{}

	// Shared HTTP client for Wikipedia requests
	wikiClient := &http.Client{Timeout: 15 * time.Second}

	enriched := 0
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}

		meta := &AlbumMetadata{
			Version:   currentSidecarVersion,
			FetchedAt: time.Now().UTC(),
			Artist:    t.Artist,
			Album:     t.Album,
		}

		// 1. Query MusicBrainz release-group
		mbMeta, err := mb.searchRelease(ctx, t.Artist, t.Album)
		if err != nil {
			m.log.Debug("musicbrainz query failed",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album),
				zap.Error(err))
		} else {
			meta.MusicBrainz = mbMeta
		}

		// 2. Query Discogs master
		dcMeta, err := dc.searchRelease(ctx, t.Artist, t.Album)
		if err != nil {
			m.log.Debug("discogs query failed",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album),
				zap.Error(err))
		} else {
			meta.Discogs = dcMeta
		}

		// 3. Discogs: fetch main release for fuller notes + credits
		if meta.Discogs != nil && meta.Discogs.MainReleaseID > 0 {
			notes, credits, err := dc.fetchRelease(ctx, meta.Discogs.MainReleaseID)
			if err != nil {
				m.log.Debug("discogs release fetch failed",
					zap.Int("release_id", meta.Discogs.MainReleaseID),
					zap.Error(err))
			} else {
				meta.Discogs.ReleaseNotes = notes
				meta.Discogs.ReleaseCredits = credits
			}
		}

		// 3b. Extract instruments from Discogs tracklist credits
		if meta.Discogs != nil && len(meta.Discogs.Credits) > 0 {
			meta.Discogs.Instruments = extractInstruments(meta.Discogs.Credits)
		}

		// 4. Fetch artist info (with caching)
		var artistInfo *ArtistInfo
		var artistWikiURL string

		// 4a. MusicBrainz artist
		if meta.MusicBrainz != nil && len(meta.MusicBrainz.ArtistIDs) > 0 {
			mbArtistID := meta.MusicBrainz.ArtistIDs[0]
			if cached, ok := mbArtistCache[mbArtistID]; ok {
				artistInfo = cached.Info
				artistWikiURL = cached.WikiURL
			} else {
				info, wikiURL, err := mb.fetchArtist(ctx, mbArtistID)
				if err != nil {
					m.log.Debug("musicbrainz artist fetch failed",
						zap.String("artist_id", mbArtistID),
						zap.Error(err))
				} else {
					artistInfo = info
					artistWikiURL = wikiURL
				}
				mbArtistCache[mbArtistID] = &artistCacheEntry{Info: artistInfo, WikiURL: artistWikiURL}
			}
		}

		// 4b. Discogs artist — biography and members
		if meta.Discogs != nil && meta.Discogs.ArtistID > 0 {
			dcArtistID := meta.Discogs.ArtistID
			if cached, ok := dcArtistCache[dcArtistID]; ok {
				if artistInfo == nil {
					artistInfo = cached.Info
				} else {
					// Merge Discogs data into existing MB artist info
					if cached.Info != nil {
						artistInfo.Biography = cached.Info.Biography
						artistInfo.Members = cached.Info.Members
					}
				}
			} else {
				dcArtResp, err := dc.fetchArtist(ctx, dcArtistID)
				if err != nil {
					m.log.Debug("discogs artist fetch failed",
						zap.Int("artist_id", dcArtistID),
						zap.Error(err))
					dcArtistCache[dcArtistID] = &artistCacheEntry{}
				} else {
					dcInfo := &ArtistInfo{
						Name:      dcArtResp.Name,
						Biography: dcArtResp.Profile,
					}
					for _, member := range dcArtResp.Members {
						if member.Name != "" {
							dcInfo.Members = append(dcInfo.Members, member.Name)
						}
					}
					dcArtistCache[dcArtistID] = &artistCacheEntry{Info: dcInfo}

					if artistInfo == nil {
						artistInfo = dcInfo
					} else {
						artistInfo.Biography = dcArtResp.Profile
						for _, member := range dcArtResp.Members {
							if member.Name != "" {
								artistInfo.Members = append(artistInfo.Members, member.Name)
							}
						}
					}
				}
			}
		}

		// 5. Wikipedia summaries
		var albumWikiSummary string

		// 5a. Album Wikipedia (from MB release-group url-rels)
		if meta.MusicBrainz != nil && meta.MusicBrainz.WikipediaURL != "" {
			summary, err := fetchWikipediaSummary(ctx, wikiClient, meta.MusicBrainz.WikipediaURL)
			if err != nil {
				m.log.Debug("album wikipedia fetch failed",
					zap.String("url", meta.MusicBrainz.WikipediaURL),
					zap.Error(err))
			} else {
				albumWikiSummary = summary
			}
		}

		// 5b. Artist Wikipedia (fallback if Discogs biography empty)
		if artistInfo != nil && artistInfo.Biography == "" && artistWikiURL != "" {
			summary, err := fetchWikipediaSummary(ctx, wikiClient, artistWikiURL)
			if err != nil {
				m.log.Debug("artist wikipedia fetch failed",
					zap.String("url", artistWikiURL),
					zap.Error(err))
			} else {
				artistInfo.Biography = summary
			}
		}

		// 6. Build AlbumDescription
		if (meta.MusicBrainz != nil && meta.MusicBrainz.Annotation != "") || albumWikiSummary != "" {
			meta.Description = &AlbumDescription{
				WikipediaSummary: albumWikiSummary,
			}
			if meta.MusicBrainz != nil {
				meta.Description.MBAnnotation = meta.MusicBrainz.Annotation
			}
		}

		// 6b. Generate LLM summary
		if m.summaryGen == nil {
			m.log.Debug("summary generation skipped: generator not configured",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album))
		} else if meta.MusicBrainz == nil && meta.Discogs == nil {
			m.log.Debug("summary generation skipped: no metadata sources",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album))
		} else {
			m.log.Debug("generating album summary",
				zap.String("artist", t.Artist),
				zap.String("album", t.Album))
			summary, err := generateAlbumSummary(ctx, m.summaryGen, meta)
			if err != nil {
				m.log.Warn("summary generation failed",
					zap.String("artist", t.Artist),
					zap.String("album", t.Album),
					zap.Error(err))
			} else if summary != "" {
				if meta.Description == nil {
					meta.Description = &AlbumDescription{}
				}
				meta.Description.GeneratedSummary = summary
				m.log.Debug("album summary generated",
					zap.String("artist", t.Artist),
					zap.String("album", t.Album),
					zap.Int("length", len(summary)))
			} else {
				m.log.Debug("summary generation returned empty",
					zap.String("artist", t.Artist),
					zap.String("album", t.Album))
			}
		}

		// Set artist info
		meta.ArtistInfo = artistInfo

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

// backfillSummaries generates LLM summaries for existing sidecars that have
// metadata (MusicBrainz or Discogs) but no generated summary. This handles
// sidecars that were written before summary generation was enabled.
func (m *Module) backfillSummaries(ctx context.Context, metas map[string]*AlbumMetadata, dirs map[string]string) {
	var candidates []string
	noMetadata := 0
	alreadyHave := 0
	for key, meta := range metas {
		if meta.MusicBrainz == nil && meta.Discogs == nil {
			noMetadata++
			continue
		}
		if meta.Description != nil && meta.Description.GeneratedSummary != "" {
			alreadyHave++
			continue
		}
		candidates = append(candidates, key)
	}

	m.log.Debug("summary backfill scan",
		zap.Int("candidates", len(candidates)),
		zap.Int("already_have_summary", alreadyHave),
		zap.Int("no_metadata", noMetadata),
		zap.Int("total_sidecars", len(metas)))

	if len(candidates) == 0 {
		return
	}

	m.log.Info("summary backfill starting", zap.Int("albums", len(candidates)))
	generated := 0
	for _, key := range candidates {
		if ctx.Err() != nil {
			break
		}
		meta := metas[key]
		dir := dirs[key]

		m.log.Debug("backfill generating summary",
			zap.String("artist", meta.Artist),
			zap.String("album", meta.Album))

		summary, err := generateAlbumSummary(ctx, m.summaryGen, meta)
		if err != nil {
			m.log.Warn("backfill summary generation failed",
				zap.String("artist", meta.Artist),
				zap.String("album", meta.Album),
				zap.Error(err))
			continue
		}
		if summary == "" {
			m.log.Debug("backfill summary returned empty",
				zap.String("artist", meta.Artist),
				zap.String("album", meta.Album))
			continue
		}

		if meta.Description == nil {
			meta.Description = &AlbumDescription{}
		}
		meta.Description.GeneratedSummary = summary

		if err := writeSidecar(dir, meta); err != nil {
			m.log.Warn("backfill failed to write sidecar",
				zap.String("dir", dir),
				zap.Error(err))
			continue
		}

		m.log.Debug("backfill summary generated",
			zap.String("artist", meta.Artist),
			zap.String("album", meta.Album),
			zap.Int("length", len(summary)))
		generated++
	}

	m.log.Info("summary backfill complete",
		zap.Int("generated", generated),
		zap.Int("total", len(candidates)))

	// Rebuild embeddings if any summaries were added
	if generated > 0 {
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
