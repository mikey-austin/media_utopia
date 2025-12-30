//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/clock"
	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqtt"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/internal/modules/embedded_mqtt"
	"github.com/mikey-austin/media_utopia/internal/modules/playlist"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

type memoryLeaseStore struct {
	mu    sync.Mutex
	store map[string]mu.Lease
}

func newMemoryLeaseStore() *memoryLeaseStore {
	return &memoryLeaseStore{store: map[string]mu.Lease{}}
}

func (m *memoryLeaseStore) Get(rendererID string) (mu.Lease, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lease, ok := m.store[rendererID]
	return lease, ok, nil
}

func (m *memoryLeaseStore) Put(rendererID string, lease mu.Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[rendererID] = lease
	return nil
}

func (m *memoryLeaseStore) Clear(rendererID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, rendererID)
	return nil
}

var (
	muBinOnce sync.Once
	muBinPath string
	muBinErr  error
)

type integrationOptions struct {
	allowAnonymous bool
	username       string
	password       string
}

type integrationHarness struct {
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *slog.Logger
	brokerURL    string
	playlistNode string
	client       *mqtt.Client
	service      core.Service
}

func TestMuMudIntegration(t *testing.T) {
	h := setupIntegration(t)
	ctx := h.ctx

	h.logger.Debug("starting core playlist flow")
	nodes, err := h.service.ListNodes(ctx, "playlist", false)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].NodeID != h.playlistNode {
		t.Fatalf("expected playlist node %s, got %+v", h.playlistNode, nodes.Nodes)
	}

	if err := h.service.PlaylistCreate(ctx, "Road Trip", ""); err != nil {
		t.Fatalf("playlist create: %v", err)
	}
	listResult, err := h.service.PlaylistList(ctx, "")
	if err != nil {
		t.Fatalf("playlist list: %v", err)
	}
	playlistID := findPlaylistID(t, listResult.Playlists, "Road Trip")

	raw, err := h.service.PlaylistGet(ctx, playlistID, "")
	if err != nil {
		t.Fatalf("playlist get: %v", err)
	}
	var loaded playlist.Playlist
	if err := json.Unmarshal(rawData(t, raw), &loaded); err != nil {
		t.Fatalf("playlist decode: %v", err)
	}
	if loaded.Name != "Road Trip" || len(loaded.Entries) != 0 {
		t.Fatalf("unexpected playlist state: %+v", loaded)
	}

	items := []string{
		"https://example.com/a.mp3",
		"https://example.com/b.mp3",
	}
	if err := h.service.PlaylistAdd(ctx, playlistID, items, "no", ""); err != nil {
		t.Fatalf("playlist add: %v", err)
	}
	raw, err = h.service.PlaylistGet(ctx, playlistID, "")
	if err != nil {
		t.Fatalf("playlist get after add: %v", err)
	}
	if err := json.Unmarshal(rawData(t, raw), &loaded); err != nil {
		t.Fatalf("playlist decode after add: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Resolved == nil || loaded.Entries[0].Resolved.URL != items[0] {
		t.Fatalf("expected entry url %s, got %+v", items[0], loaded.Entries[0])
	}
	if loaded.Entries[1].Resolved == nil || loaded.Entries[1].Resolved.URL != items[1] {
		t.Fatalf("expected entry url %s, got %+v", items[1], loaded.Entries[1])
	}

	if err := h.service.PlaylistRemove(ctx, playlistID, []string{loaded.Entries[0].EntryID}, ""); err != nil {
		t.Fatalf("playlist remove: %v", err)
	}
	raw, err = h.service.PlaylistGet(ctx, playlistID, "")
	if err != nil {
		t.Fatalf("playlist get after remove: %v", err)
	}
	if err := json.Unmarshal(rawData(t, raw), &loaded); err != nil {
		t.Fatalf("playlist decode after remove: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry after remove, got %d", len(loaded.Entries))
	}

	if err := h.service.PlaylistRename(ctx, playlistID, "Updated", ""); err != nil {
		t.Fatalf("playlist rename: %v", err)
	}
	listResult, err = h.service.PlaylistList(ctx, "")
	if err != nil {
		t.Fatalf("playlist list after rename: %v", err)
	}
	_ = findPlaylistID(t, listResult.Playlists, "Updated")

	suggests, err := h.service.SuggestList(ctx, "")
	if err != nil {
		t.Fatalf("suggest list: %v", err)
	}
	if len(suggests.Suggestions) != 0 {
		t.Fatalf("expected no suggestions, got %+v", suggests.Suggestions)
	}

	snapshots, err := h.service.SnapshotList(ctx, "")
	if err != nil {
		t.Fatalf("snapshot list: %v", err)
	}
	if len(snapshots.Snapshots) != 0 {
		t.Fatalf("expected no snapshots, got %+v", snapshots.Snapshots)
	}
}

func TestPlaylistConflictReturnsError(t *testing.T) {
	h := setupIntegration(t)
	ctx := h.ctx

	if err := h.service.PlaylistCreate(ctx, "Conflict", ""); err != nil {
		t.Fatalf("playlist create: %v", err)
	}
	listResult, err := h.service.PlaylistList(ctx, "")
	if err != nil {
		t.Fatalf("playlist list: %v", err)
	}
	playlistID := findPlaylistID(t, listResult.Playlists, "Conflict")

	cmd, err := mu.NewCommand("playlist.addItems", mu.PlaylistAddItemsBody{
		PlaylistID: playlistID,
		Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "https://example.com/a.mp3"}},
		},
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}
	wrongRev := int64(999)
	cmd = decorateCommand(h, cmd)
	cmd.IfRevision = &wrongRev
	reply := publishCommand(t, h, cmd)
	if reply.Err == nil || reply.Err.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT, got %+v", reply.Err)
	}
}

func TestPlaylistNotFoundErrors(t *testing.T) {
	h := setupIntegration(t)

	cmd, err := mu.NewCommand("playlist.get", mu.PlaylistGetBody{PlaylistID: "mu:playlist:missing"})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}
	reply := publishCommand(t, h, decorateCommand(h, cmd))
	if reply.Err == nil || reply.Err.Code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND, got %+v", reply.Err)
	}

	cmd, err = mu.NewCommand("suggest.get", mu.SuggestGetBody{SuggestionID: "mu:suggest:missing"})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}
	reply = publishCommand(t, h, decorateCommand(h, cmd))
	if reply.Err == nil || reply.Err.Code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND, got %+v", reply.Err)
	}
}

func TestInvalidCommandReturnsError(t *testing.T) {
	h := setupIntegration(t)

	cmd, err := mu.NewCommand("playlist.unknown", struct{}{})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}
	reply := publishCommand(t, h, decorateCommand(h, cmd))
	if reply.Err == nil || reply.Err.Code != "INVALID" {
		t.Fatalf("expected INVALID, got %+v", reply.Err)
	}
}

func TestPlaylistRevisionGuardAcceptsMatch(t *testing.T) {
	h := setupIntegration(t)
	ctx := h.ctx

	if err := h.service.PlaylistCreate(ctx, "Revisioned", ""); err != nil {
		t.Fatalf("playlist create: %v", err)
	}
	listResult, err := h.service.PlaylistList(ctx, "")
	if err != nil {
		t.Fatalf("playlist list: %v", err)
	}
	playlistID := findPlaylistID(t, listResult.Playlists, "Revisioned")

	raw, err := h.service.PlaylistGet(ctx, playlistID, "")
	if err != nil {
		t.Fatalf("playlist get: %v", err)
	}
	var loaded playlist.Playlist
	if err := json.Unmarshal(rawData(t, raw), &loaded); err != nil {
		t.Fatalf("playlist decode: %v", err)
	}
	startRev := loaded.Revision

	cmd, err := mu.NewCommand("playlist.addItems", mu.PlaylistAddItemsBody{
		PlaylistID: playlistID,
		Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "https://example.com/ok.mp3"}},
		},
	})
	if err != nil {
		t.Fatalf("build command: %v", err)
	}
	cmd = decorateCommand(h, cmd)
	cmd.IfRevision = &startRev
	reply := publishCommand(t, h, cmd)
	if !reply.OK || reply.Err != nil {
		t.Fatalf("expected ok reply, got %+v", reply.Err)
	}

	raw, err = h.service.PlaylistGet(ctx, playlistID, "")
	if err != nil {
		t.Fatalf("playlist get after add: %v", err)
	}
	if err := json.Unmarshal(rawData(t, raw), &loaded); err != nil {
		t.Fatalf("playlist decode after add: %v", err)
	}
	if loaded.Revision != startRev+1 {
		t.Fatalf("expected revision %d, got %d", startRev+1, loaded.Revision)
	}
}

func TestMuCLIIntegration(t *testing.T) {
	h := setupIntegration(t)
	muPath := muBinary(t)
	env := cliEnv(t)
	baseArgs := []string{
		"--broker", h.brokerURL,
		"--topic-base", mu.BaseTopic,
		"--identity", "integration-cli",
		"--timeout", "3s",
	}

	out := runMu(t, muPath, env, append(baseArgs, "--json", "ls", "--kind", "playlist")...)
	var nodes core.NodesResult
	decodeJSON(t, out, &nodes)
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].NodeID != h.playlistNode {
		t.Fatalf("expected playlist node %s, got %+v", h.playlistNode, nodes.Nodes)
	}

	runMu(t, muPath, env, append(baseArgs, "playlist", "create", "CLI Road Trip", "--server", h.playlistNode)...)

	out = runMu(t, muPath, env, append(baseArgs, "--json", "playlist", "ls", "--server", h.playlistNode)...)
	var listOut core.PlaylistListResult
	decodeJSON(t, out, &listOut)
	playlistID := findPlaylistID(t, listOut.Playlists, "CLI Road Trip")

	out = runMu(t, muPath, env, append(baseArgs, "--json", "playlist", "show", playlistID, "--server", h.playlistNode)...)
	var rawOut core.RawResult
	decodeJSON(t, out, &rawOut)
	var playlistOut playlist.Playlist
	if err := json.Unmarshal(rawData(t, rawOut), &playlistOut); err != nil {
		t.Fatalf("decode playlist: %v", err)
	}
	if playlistOut.Name != "CLI Road Trip" {
		t.Fatalf("expected playlist name, got %q", playlistOut.Name)
	}

	runMu(t, muPath, env, append(baseArgs, "playlist", "add", playlistID, "https://example.com/cli.mp3", "--resolve", "no", "--server", h.playlistNode)...)

	out = runMu(t, muPath, env, append(baseArgs, "--json", "playlist", "show", playlistID, "--server", h.playlistNode)...)
	decodeJSON(t, out, &rawOut)
	if err := json.Unmarshal(rawData(t, rawOut), &playlistOut); err != nil {
		t.Fatalf("decode playlist after add: %v", err)
	}
	if len(playlistOut.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(playlistOut.Entries))
	}
	if playlistOut.Entries[0].Resolved == nil || playlistOut.Entries[0].Resolved.URL != "https://example.com/cli.mp3" {
		t.Fatalf("unexpected entry: %+v", playlistOut.Entries[0])
	}
}

func TestEmbeddedMQTTAuth(t *testing.T) {
	h := setupIntegrationWithOptions(t, integrationOptions{
		allowAnonymous: false,
		username:       "muuser",
		password:       "mupass",
	})

	unauth, err := mqtt.NewClient(mqtt.Options{
		BrokerURL: h.brokerURL,
		ClientID:  "mu-int-unauth-" + idgen.Generator{}.NewID(),
		TopicBase: mu.BaseTopic,
		Timeout:   500 * time.Millisecond,
	})
	if err == nil {
		_ = unauth
		t.Fatalf("expected unauthenticated connection to fail")
	}

	if _, err := h.service.ListNodes(h.ctx, "playlist", false); err != nil {
		t.Fatalf("authenticated list nodes: %v", err)
	}
}

func setupIntegration(t *testing.T) *integrationHarness {
	return setupIntegrationWithOptions(t, integrationOptions{allowAnonymous: true})
}

func setupIntegrationWithOptions(t *testing.T, opts integrationOptions) *integrationHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	logger := testLogger()
	listen := freeListenAddr(t)
	brokerURL := embeddedmqtt.BrokerURL(listen, false)

	mqttModule, err := embeddedmqtt.NewModule(logger, embeddedmqtt.Config{
		Listen:         listen,
		AllowAnonymous: opts.allowAnonymous,
		Username:       opts.username,
		Password:       opts.password,
	})
	if err != nil {
		t.Fatalf("embedded mqtt module: %v", err)
	}
	runModule(t, ctx, "embedded_mqtt", mqttModule.Run)
	waitForBrokerReady(t, listen)

	serverClient := waitForMQTTServerClient(t, brokerURL, opts.username, opts.password)
	playlistDir := t.TempDir()
	playlistNode := fmt.Sprintf("mu:playlist:plsrv:integration:%s", idgen.Generator{}.NewID())
	playlistModule, err := playlist.NewModule(logger, serverClient, playlist.Config{
		NodeID:      playlistNode,
		TopicBase:   mu.BaseTopic,
		StoragePath: playlistDir,
		Identity:    "integration",
	})
	if err != nil {
		t.Fatalf("playlist module: %v", err)
	}
	runModule(t, ctx, "playlist", playlistModule.Run)

	client := waitForMQTTClient(t, brokerURL, opts.username, opts.password)
	cfg := core.Config{
		Identity:  "integration",
		TopicBase: mu.BaseTopic,
		Defaults: core.Defaults{
			PlaylistServer: playlistNode,
		},
	}
	service := core.Service{
		Broker:     client,
		Resolver:   core.Resolver{Presence: client, Config: cfg},
		Clock:      clock.Clock{},
		IDGen:      idgen.Generator{},
		LeaseStore: newMemoryLeaseStore(),
		Config:     cfg,
	}

	waitForPresence(t, client, playlistNode)
	return &integrationHarness{
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
		brokerURL:    brokerURL,
		playlistNode: playlistNode,
		client:       client,
		service:      service,
	}
}

func runModule(t *testing.T, ctx context.Context, name string, run func(context.Context) error) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx)
	}()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s module failed: %v", name, err)
		}
	default:
	}
	t.Cleanup(func() {
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("%s module failed: %v", name, err)
			}
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func waitForMQTTClient(t *testing.T, brokerURL string, username string, password string) *mqtt.Client {
	t.Helper()
	gen := idgen.Generator{}
	var lastErr error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err := mqtt.NewClient(mqtt.Options{
			BrokerURL: brokerURL,
			ClientID:  "mu-int-" + gen.NewID(),
			TopicBase: mu.BaseTopic,
			Timeout:   2 * time.Second,
			Username:  username,
			Password:  password,
		})
		if err == nil {
			return client
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("connect mu client: %v", lastErr)
	return nil
}

func waitForMQTTServerClient(t *testing.T, brokerURL string, username string, password string) *mqttserver.Client {
	t.Helper()
	gen := idgen.Generator{}
	var lastErr error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err := mqttserver.NewClient(mqttserver.Options{
			BrokerURL: brokerURL,
			ClientID:  "mud-int-" + gen.NewID(),
			Timeout:   2 * time.Second,
			Username:  username,
			Password:  password,
		})
		if err == nil {
			return client
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("connect mqtt server client: %v", lastErr)
	return nil
}

func waitForPresence(t *testing.T, client *mqtt.Client, nodeID string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		presence, err := client.ListPresence(context.Background())
		if err == nil {
			for _, p := range presence {
				if p.NodeID == nodeID {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for presence: %s", nodeID)
}

func findPlaylistID(t *testing.T, playlists []mu.PlaylistSummary, name string) string {
	t.Helper()
	for _, pl := range playlists {
		if pl.Name == name {
			return pl.PlaylistID
		}
	}
	t.Fatalf("playlist %q not found in %+v", name, playlists)
	return ""
}

func rawData(t *testing.T, raw core.RawResult) []byte {
	t.Helper()
	switch data := raw.Data.(type) {
	case json.RawMessage:
		return data
	case []byte:
		return data
	case map[string]any:
		payload, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal raw map: %v", err)
		}
		return payload
	default:
		t.Fatalf("unexpected raw data type %T", raw.Data)
		return nil
	}
}

func freeListenAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("network listen not permitted in this environment")
		}
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

func waitForBrokerReady(t *testing.T, listen string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", listen, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if errors.Is(err, syscall.EPERM) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("network dial not permitted in this environment")
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("broker not ready: %v", lastErr)
}

func publishCommand(t *testing.T, h *integrationHarness, cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 3*time.Second)
	t.Cleanup(cancel)
	h.logger.Debug("publish command", "type", cmd.Type, "id", cmd.ID, "node", h.playlistNode)
	reply, err := h.client.PublishCommand(ctx, h.playlistNode, cmd)
	if err != nil {
		t.Fatalf("publish command: %v", err)
	}
	h.logger.Debug("command reply", "ok", reply.OK, "err", reply.Err)
	return reply
}

func decorateCommand(h *integrationHarness, cmd mu.CommandEnvelope) mu.CommandEnvelope {
	cmd.ID = idgen.Generator{}.NewID()
	cmd.TS = time.Now().Unix()
	cmd.From = "integration"
	cmd.ReplyTo = h.client.ReplyTopic()
	return cmd
}

func testLogger() *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("MU_INTEGRATION_DEBUG"), "1") || strings.EqualFold(os.Getenv("MU_INTEGRATION_DEBUG"), "true") {
		level = slog.LevelDebug
	}
	writer := io.Discard
	if level == slog.LevelDebug {
		writer = os.Stderr
	}
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level}))
}

func decodeJSON(t *testing.T, payload string, dest any) {
	t.Helper()
	if err := json.Unmarshal([]byte(payload), dest); err != nil {
		t.Fatalf("decode json: %v\npayload: %s", err, payload)
	}
}

func runMu(t *testing.T, muPath string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(muPath, args...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("mu %s failed: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

func cliEnv(t *testing.T) []string {
	t.Helper()
	cfgDir := t.TempDir()
	stateDir := t.TempDir()
	env := append([]string{}, os.Environ()...)
	env = append(env, "XDG_CONFIG_HOME="+cfgDir)
	env = append(env, "XDG_STATE_HOME="+stateDir)
	return env
}

func muBinary(t *testing.T) string {
	t.Helper()
	muBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mu-cli-bin-*")
		if err != nil {
			muBinErr = err
			return
		}
		binPath := filepath.Join(dir, "mu")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/mu")
		cmd.Dir = repoRoot(t)
		output, err := cmd.CombinedOutput()
		if err != nil {
			muBinErr = fmt.Errorf("build mu: %w: %s", err, strings.TrimSpace(string(output)))
			return
		}
		muBinPath = binPath
	})
	if muBinErr != nil {
		t.Fatalf("build mu binary: %v", muBinErr)
	}
	return muBinPath
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", dir)
		}
		dir = parent
	}
}
