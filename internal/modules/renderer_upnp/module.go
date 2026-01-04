//go:build upnp

package rendererupnp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/internal/adapters/pupnp"
	"github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the UPnP renderer bridge.
type Config struct {
	TopicBase         string
	Provider          string
	Namespace         string
	Listen            string
	DiscoveryInterval time.Duration
	Timeout           time.Duration
	NamePrefix        string
}

// Enabled indicates upnp build is active.
const Enabled = true

// Module manages UPnP renderers.
type Module struct {
	log     *zap.Logger
	client  mqttClient
	http    *http.Client
	upnp    *pupnp.Client
	config  Config
	render  map[string]*rendererInstance // keyed by device ID
	mu      sync.Mutex
	closing chan struct{}
}

type mqttClient interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
	Subscribe(topic string, qos byte, handler paho.MessageHandler) error
	Unsubscribe(topic string) error
}

type rendererInstance struct {
	deviceID      string
	resource      string
	nodeID        string
	name          string
	engine        *renderercore.Engine
	cmdTopic      string
	avTransport   string
	avtService    string
	renderControl string
	rcService     string
	baseURL       string
}

// NewModule creates a UPnP renderer module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return nil, errors.New("provider required")
	}
	if strings.TrimSpace(cfg.Namespace) == "" {
		return nil, errors.New("namespace required")
	}
	if strings.TrimSpace(cfg.Listen) == "" {
		cfg.Listen = "0.0.0.0:0"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.DiscoveryInterval == 0 {
		cfg.DiscoveryInterval = 5 * time.Minute
	}
	upnpClient, err := pupnp.NewClient(cfg.Listen)
	if err != nil {
		return nil, err
	}
	return &Module{
		log:    log,
		client: client,
		http: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
		upnp:    upnpClient,
		config:  cfg,
		render:  map[string]*rendererInstance{},
		closing: make(chan struct{}),
	}, nil
}

// Run starts discovery and subscriptions.
func (m *Module) Run(ctx context.Context) error {
	defer m.upnp.Close()
	go m.discoveryLoop(ctx)
	<-ctx.Done()
	close(m.closing)
	return nil
}

func (m *Module) discoveryLoop(ctx context.Context) {
	m.refresh()
	ticker := time.NewTicker(m.config.DiscoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refresh()
		}
	}
}

func (m *Module) refresh() {
	results, err := m.upnp.Discover(context.Background(), "urn:schemas-upnp-org:device:MediaRenderer:1", 3*time.Second)
	if err != nil {
		m.log.Debug("upnp renderer discover failed", zap.Error(err))
		return
	}
	now := time.Now()
	for _, res := range results {
		dev, derr := m.describe(res.Location)
		if derr != nil {
			m.log.Debug("describe renderer failed", zap.Error(derr), zap.String("location", res.Location))
			continue
		}
		m.mu.Lock()
		if _, ok := m.render[dev.deviceID]; !ok {
			m.render[dev.deviceID] = dev
			m.log.Info("upnp renderer discovered",
				zap.String("name", dev.name),
				zap.String("resource", dev.resource),
				zap.String("device_id", dev.deviceID),
				zap.String("avtransport", dev.avTransport),
				zap.String("rendercontrol", dev.renderControl))
			go m.startRenderer(dev)
		}
		m.mu.Unlock()
		dev.engine.State.TS = now.Unix()
	}
}

func (m *Module) describe(location string) (*rendererInstance, error) {
	req, err := http.NewRequest("GET", location, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("describe renderer error: %s", resp.Status)
	}
	var desc deviceDescription
	if err := xml.NewDecoder(resp.Body).Decode(&desc); err != nil {
		return nil, err
	}
	base := desc.BaseURL(location)
	avt, avtService, rc, rcService := desc.ControlURLs(base)
	if avt == "" || rc == "" {
		return nil, errors.New("missing AVTransport or RenderingControl")
	}
	friendly := desc.Device.FriendlyName
	if strings.TrimSpace(m.config.NamePrefix) != "" {
		friendly = strings.TrimSpace(m.config.NamePrefix) + " " + friendly
	}
	resource := sanitizeResource(friendly)
	deviceID := strings.TrimPrefix(desc.Device.UDN, "uuid:")
	resource = m.ensureUniqueResource(resource)
	nodeID := fmt.Sprintf("mu:renderer:%s:%s:%s", m.config.Provider, m.config.Namespace, resource)
	engine := renderercore.NewEngine(nodeID, friendly, &upnpDriver{
		log:        m.log.With(zap.String("renderer", resource)),
		http:       m.http,
		avtURL:     avt,
		rcURL:      rc,
		avtService: avtService,
		rcService:  rcService,
	})
	cmdTopic := mu.TopicCommands(m.config.TopicBase, nodeID)
	return &rendererInstance{
		deviceID:      deviceID,
		resource:      resource,
		nodeID:        nodeID,
		name:          friendly,
		engine:        engine,
		cmdTopic:      cmdTopic,
		avTransport:   avt,
		avtService:    avtService,
		renderControl: rc,
		rcService:     rcService,
		baseURL:       base,
	}, nil
}

func (m *Module) startRenderer(r *rendererInstance) {
	if err := m.publishPresence(r); err != nil {
		m.log.Warn("upnp renderer presence failed", zap.String("node", r.nodeID), zap.Error(err))
		return
	}
	go m.stateLoop(r)
	handler := func(_ paho.Client, msg paho.Message) {
		m.handleRendererMessage(r, msg)
	}
	if err := m.client.Subscribe(r.cmdTopic, 1, handler); err != nil {
		m.log.Warn("upnp renderer subscribe failed", zap.String("topic", r.cmdTopic), zap.Error(err))
		return
	}
}

func (m *Module) stateLoop(r *rendererInstance) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.closing:
			return
		case <-ticker.C:
			pos, dur, ok := r.engine.Driver.Position()
			if !ok {
				continue
			}
			changed := false
			if r.engine.State.Playback.PositionMS != pos {
				r.engine.State.Playback.PositionMS = pos
				changed = true
			}
			if r.engine.State.Playback.DurationMS != dur {
				r.engine.State.Playback.DurationMS = dur
				changed = true
			}
			if changed {
				if payload, err := r.engineStatePayload(); err == nil {
					_ = m.client.Publish(mu.TopicState(m.config.TopicBase, r.nodeID), 1, true, payload)
				}
			}
		}
	}
}

func (m *Module) handleRendererMessage(r *rendererInstance, msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid renderer command", zap.String("node", r.nodeID), zap.Error(err))
		return
	}
	reply := r.engine.HandleCommand(cmd)
	if cmd.ReplyTo != "" {
		if payload, err := json.Marshal(reply); err == nil {
			_ = m.client.Publish(cmd.ReplyTo, 1, false, payload)
		}
	}
	if payload, err := r.engineStatePayload(); err == nil {
		_ = m.client.Publish(mu.TopicState(m.config.TopicBase, r.nodeID), 1, true, payload)
	}
}

func (r *rendererInstance) engineStatePayload() ([]byte, error) {
	state := r.engine.State
	state.TS = time.Now().Unix()
	return json.Marshal(state)
}

func (m *Module) publishPresence(r *rendererInstance) error {
	presence := mu.Presence{
		NodeID: r.nodeID,
		Kind:   "renderer",
		Name:   r.name,
		Caps: map[string]any{
			"queueResolve": false,
			"seek":         true,
			"volume":       true,
		},
		TS: time.Now().Unix(),
	}
	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, r.nodeID), 1, true, payload)
}

func (m *Module) ensureUniqueResource(base string) string {
	resource := base
	i := 1
	for {
		unique := true
		for _, r := range m.render {
			if r.resource == resource {
				unique = false
				break
			}
		}
		if unique {
			return resource
		}
		i++
		resource = fmt.Sprintf("%s_%d", base, i)
	}
}

var sanitizeRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func sanitizeResource(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = sanitizeRe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_-")
	if name == "" {
		return "upnp_renderer"
	}
	return name
}

// Device description parsing (reuse minimal subset).
type deviceDescription struct {
	URLBase string `xml:"URLBase"`
	Device  struct {
		FriendlyName string          `xml:"friendlyName"`
		UDN          string          `xml:"UDN"`
		Services     []deviceService `xml:"serviceList>service"`
	} `xml:"device"`
}

type deviceService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

func (d deviceDescription) BaseURL(location string) string {
	if strings.TrimSpace(d.URLBase) != "" {
		return strings.TrimRight(d.URLBase, "/")
	}
	u, err := url.Parse(location)
	if err != nil {
		return location
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

func (d deviceDescription) ControlURLs(base string) (avTransport string, avTransportService string, renderControl string, renderControlService string) {
	for _, svc := range d.Device.Services {
		if strings.Contains(strings.ToLower(svc.ServiceType), "avtransport") && avTransport == "" {
			avTransport = resolveURL(base, svc.ControlURL)
			avTransportService = svc.ServiceType
		}
		if strings.Contains(strings.ToLower(svc.ServiceType), "renderingcontrol") && renderControl == "" {
			renderControl = resolveURL(base, svc.ControlURL)
			renderControlService = svc.ServiceType
		}
		if renderControl != "" && avTransport != "" {
			break
		}
	}
	return
}

func resolveURL(baseURL string, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + ref
	}
	rel, err := url.Parse(ref)
	if err != nil {
		base.Path = path.Join(base.Path, ref)
		return base.String()
	}
	return base.ResolveReference(rel).String()
}

// upnpDriver implements renderer_core.Driver using AVTransport and RenderingControl.
type upnpDriver struct {
	log        *zap.Logger
	http       *http.Client
	avtURL     string
	rcURL      string
	avtService string
	rcService  string
}

func (d *upnpDriver) Play(url string, positionMS int64) error {
	d.log.Debug("upnp play request", zap.String("url", url), zap.Int64("position_ms", positionMS))
	if err := d.setAVTransportURI(url); err != nil {
		return fmt.Errorf("setAVTransportURI: %w", err)
	}
	if positionMS > 0 {
		if err := d.seek(positionMS); err != nil {
			d.log.Debug("seek failed", zap.Error(err))
		}
	}
	if err := d.avtAction("Play", map[string]string{"Speed": "1"}); err != nil {
		return fmt.Errorf("play action: %w", err)
	}
	return nil
}

func (d *upnpDriver) Pause() error  { return d.avtAction("Pause", nil) }
func (d *upnpDriver) Resume() error { return d.avtAction("Play", map[string]string{"Speed": "1"}) }
func (d *upnpDriver) Stop() error   { return d.avtAction("Stop", nil) }

func (d *upnpDriver) Seek(positionMS int64) error {
	return d.seek(positionMS)
}

func (d *upnpDriver) SetVolume(volume float64) error {
	val := int(volume * 100)
	if val < 0 {
		val = 0
	}
	if val > 100 {
		val = 100
	}
	return d.rcAction("SetVolume", map[string]string{
		"Channel":       "Master",
		"DesiredVolume": fmt.Sprintf("%d", val),
	})
}

func (d *upnpDriver) SetMute(mute bool) error {
	on := "0"
	if mute {
		on = "1"
	}
	return d.rcAction("SetMute", map[string]string{
		"Channel":     "Master",
		"DesiredMute": on,
	})
}

func (d *upnpDriver) Position() (int64, int64, bool) {
	type posResp struct {
		RelTime       string `xml:"Body>GetPositionInfoResponse>RelTime"`
		TrackDuration string `xml:"Body>GetPositionInfoResponse>TrackDuration"`
	}
	service := normalizeService(d.avtService)
	if strings.TrimSpace(service) == "" {
		service = normalizeService(serviceFromURL(d.avtURL))
	}
	body, err := d.soapRequest(d.avtURL, "GetPositionInfo", service, map[string]string{})
	if err != nil {
		return 0, 0, false
	}
	var resp posResp
	if err := xml.Unmarshal(body, &resp); err != nil {
		return 0, 0, false
	}
	pos := parseRelTime(resp.RelTime)
	dur := parseRelTime(resp.TrackDuration)
	return pos, dur, true
}

func (d *upnpDriver) setAVTransportURI(mediaURL string) error {
	meta := buildDIDL(mediaURL, guessMime(mediaURL))
	params := map[string]string{
		"CurrentURI":         mediaURL,
		"CurrentURIMetaData": meta,
	}
	return d.avtAction("SetAVTransportURI", params)
}

func (d *upnpDriver) seek(positionMS int64) error {
	target := formatRelTime(positionMS)
	d.log.Debug("upnp seek", zap.Int64("position_ms", positionMS), zap.String("target", target))
	params := map[string]string{
		"Unit":   "REL_TIME",
		"Target": target,
	}
	if err := d.avtAction("Seek", params); err != nil {
		d.log.Debug("upnp seek rel_time failed, retrying abs_time", zap.Error(err), zap.String("target", target))
		if err2 := d.avtAction("Seek", map[string]string{"Unit": "ABS_TIME", "Target": target}); err2 != nil {
			return err2
		}
	}
	// Some renderers pause on seek; ensure playback continues.
	if err := d.avtAction("Play", map[string]string{"Speed": "1"}); err != nil {
		d.log.Debug("upnp seek follow-up play failed", zap.Error(err))
	}
	return nil
}

func (d *upnpDriver) avtAction(action string, params map[string]string) error {
	service := d.avtService
	if strings.TrimSpace(service) == "" {
		service = serviceFromURL(d.avtURL)
	}
	_, err := d.soapRequest(d.avtURL, action, normalizeService(service), params)
	return err
}

func (d *upnpDriver) rcAction(action string, params map[string]string) error {
	service := d.rcService
	if strings.TrimSpace(service) == "" {
		service = serviceFromURL(d.rcURL)
	}
	_, err := d.soapRequest(d.rcURL, action, normalizeService(service), params)
	return err
}

func (d *upnpDriver) soapRequest(endpoint string, action string, service string, params map[string]string) ([]byte, error) {
	envelope := buildSOAPEnvelope(action, service, params)
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest("POST", endpoint, bytes.NewReader([]byte(envelope)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("SOAPACTION", fmt.Sprintf(`"%s#%s"`, service, action))
		req.Close = true
		d.log.Debug("upnp soap request", zap.String("endpoint", endpoint), zap.String("action", action), zap.String("service", service), zap.Any("params", params))
		resp, err := d.http.Do(req)
		if err != nil {
			d.log.Debug("upnp soap request failed", zap.String("endpoint", endpoint), zap.String("action", action), zap.Error(err), zap.Int("attempt", attempt))
			if attempt == 0 && strings.Contains(err.Error(), "EOF") {
				continue
			}
			return nil, err
		}
		defer resp.Body.Close()
		body, _ := ioReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			d.log.Debug("upnp soap error", zap.String("endpoint", endpoint), zap.String("action", action), zap.String("service", service), zap.String("status", resp.Status), zap.String("body", truncateBody(string(body), 512)))
			return nil, fmt.Errorf("upnp error: %s", resp.Status)
		}
		d.log.Debug("upnp soap response", zap.String("endpoint", endpoint), zap.String("action", action), zap.String("service", service), zap.Int("bytes", len(body)))
		return body, nil
	}
	return nil, fmt.Errorf("upnp soap request failed after retry")
}

func serviceFromURL(u string) string {
	if strings.Contains(strings.ToLower(u), "render") {
		return "RenderingControl:1"
	}
	return "AVTransport:1"
}

func normalizeService(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return "urn:schemas-upnp-org:service:AVTransport:1"
	}
	if strings.HasPrefix(service, "urn:") {
		return service
	}
	return "urn:schemas-upnp-org:service:" + service
}

func buildSOAPEnvelope(action string, service string, params map[string]string) string {
	serviceNS := normalizeService(service)
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?>`)
	buf.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	buf.WriteString(`<s:Body><u:` + action + ` xmlns:u="` + serviceNS + `">`)
	buf.WriteString(`<InstanceID>0</InstanceID>`)
	for k, v := range params {
		buf.WriteString(fmt.Sprintf("<%s>%s</%s>", k, xmlEscape(v), k))
	}
	buf.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)
	return buf.String()
}

func buildDIDL(mediaURL string, mimeType string) string {
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	title := mediaURL
	if u, err := url.Parse(mediaURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
			title = base
		} else if u.Host != "" {
			title = u.Host
		}
	}
	protocol := fmt.Sprintf("http-get:*:%s:*", mimeType)
	return `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		`<item id="0" parentID="0" restricted="0">` +
		`<dc:title>` + xmlEscape(title) + `</dc:title>` +
		`<res protocolInfo="` + xmlEscape(protocol) + `">` + xmlEscape(mediaURL) + `</res>` +
		`</item></DIDL-Lite>`
}

func guessMime(mediaURL string) string {
	if u, err := url.Parse(mediaURL); err == nil {
		if ext := path.Ext(u.Path); ext != "" {
			if mt := mime.TypeByExtension(ext); mt != "" {
				return mt
			}
		}
	}
	return "application/octet-stream"
}

func xmlEscape(s string) string {
	repl := strings.NewReplacer(
		`&`, "&amp;",
		`<`, "&lt;",
		`>`, "&gt;",
		`"`, "&quot;",
		`'`, "&apos;",
	)
	return repl.Replace(s)
}

func parseRelTime(value string) int64 {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0
	}
	h := parseInt(parts[0])
	m := parseInt(parts[1])
	s := parseInt(parts[2])
	return (h*3600 + m*60 + s) * 1000
}

func formatRelTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSec := ms / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func parseInt(val string) int64 {
	out, _ := strconv.ParseInt(val, 10, 64)
	return out
}

func truncateBody(body string, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return body
	}
	return body[:limit]
}

func ioReadAll(body io.Reader) ([]byte, error) {
	return io.ReadAll(body)
}
