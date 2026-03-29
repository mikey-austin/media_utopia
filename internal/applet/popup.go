//go:build gtk

package applet

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/gtk"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// CommandFunc sends a command via MQTT.
type CommandFunc func(cmdType string, body interface{})

// LeaseFunc acquires or releases the session lease.
type LeaseFunc func()

// Popup is the mini-player popup window.
type Popup struct {
	win *gtk.Window

	// Widgets
	titleLabel  *gtk.Label
	artistLabel *gtk.Label
	albumLabel  *gtk.Label
	artworkImg  *gtk.Image
	seekBar     *gtk.Scale
	posLabel    *gtk.Label
	durLabel    *gtk.Label
	playBtn     *gtk.Button
	prevBtn     *gtk.Button
	nextBtn     *gtk.Button
	volumeBar   *gtk.Scale
	volumeLabel *gtk.Label
	queueBox    *gtk.Box
	headerBox   *gtk.Box
	statusLabel *gtk.Label
	leaseBtn    *gtk.Button
	leaseLabel  *gtk.Label

	sendCmd        CommandFunc
	onLeaseAcquire LeaseFunc
	onLeaseRelease LeaseFunc

	// State tracking
	seeking    bool
	lastStatus string
	hasLease   bool

	// Queue cache — populated via queue.get command
	queueItems []mu.QueueItem
	queueIndex int64
}

// NewPopup creates the mini-player popup window.
func NewPopup(sendCmd CommandFunc, onLeaseAcquire, onLeaseRelease LeaseFunc) (*Popup, error) {
	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return nil, err
	}
	win.SetSizeRequest(320, -1)
	win.SetResizable(false)
	win.SetDecorated(false)
	win.SetSkipTaskbarHint(true)
	win.SetSkipPagerHint(true)
	win.SetKeepAbove(true)
	win.SetTypeHint(gdk.WINDOW_TYPE_HINT_UTILITY)

	p := &Popup{
		win:            win,
		sendCmd:        sendCmd,
		onLeaseAcquire: onLeaseAcquire,
		onLeaseRelease: onLeaseRelease,
		hasLease:       true,
	}
	if err := p.buildUI(); err != nil {
		return nil, err
	}

	// Hide on Escape key
	win.Connect("key-press-event", func(_ *gtk.Window, ev *gdk.Event) bool {
		keyEvent := gdk.EventKeyNewFromEvent(ev)
		if keyEvent.KeyVal() == 0xff1b { // GDK_KEY_Escape
			p.Hide()
			return true
		}
		return false
	})

	return p, nil
}

// IsVisible reports whether the popup is currently shown.
func (p *Popup) IsVisible() bool {
	return p.win.IsVisible()
}

// Hide hides the popup window.
func (p *Popup) Hide() {
	p.win.Hide()
}

// ShowAt positions the popup near the given geometry and shows it.
func (p *Popup) ShowAt(x, y, width, height int) {
	// Position the popup above the tray icon area, centered horizontally.
	_, pw := p.win.GetPreferredWidth()
	popupX := x + (width-pw)/2
	popupY := y - 10 // just above the icon area

	p.win.Move(popupX, popupY)
	p.win.ShowAll()
	p.win.GrabFocus()
}

// ShowCentered shows the popup near the top-right of screen (near i3bar tray).
func (p *Popup) ShowCentered() {
	// Show first so GTK computes the actual size
	p.win.ShowAll()

	alloc := p.win.GetAllocation()
	pw := alloc.GetWidth()
	if pw <= 1 {
		pw = 300
	}

	screen, err := gdk.ScreenGetDefault()
	if err != nil {
		return
	}
	sw := screen.GetWidth()

	// Anchor to top-right, just below the i3bar
	popupX := sw - pw - 4
	popupY := 24

	p.win.Move(popupX, popupY)
	p.win.GrabFocus()
}

// SetQueueItems updates the cached queue data.
func (p *Popup) SetQueueItems(items []mu.QueueItem, index int64) {
	p.queueItems = items
	p.queueIndex = index
}

// SetHasLease updates the lease indicator and button.
func (p *Popup) SetHasLease(has bool) {
	p.hasLease = has
	if has {
		p.leaseLabel.SetText("● Control: active")
		sc, _ := p.leaseLabel.GetStyleContext()
		sc.RemoveClass("lease-inactive")
		sc.AddClass("lease-active")
		p.leaseBtn.SetLabel("Release")
	} else {
		p.leaseLabel.SetText("○ Control: released")
		sc, _ := p.leaseLabel.GetStyleContext()
		sc.RemoveClass("lease-active")
		sc.AddClass("lease-inactive")
		p.leaseBtn.SetLabel("Take Control")
	}
}

// UpdateState refreshes all widgets from the renderer state.
func (p *Popup) UpdateState(state *mu.RendererState) {
	if state == nil {
		return
	}

	// Track info
	if state.Current != nil {
		md := state.Current.Metadata
		title := metaString(md, "title", "")
		if title == "" {
			title = displayName(state.Current.ItemID)
		}
		p.titleLabel.SetText(title)
		artist := metaString(md, "artist", "")
		album := metaString(md, "album", "")
		if artist != "" && album != "" {
			p.artistLabel.SetText(artist)
			p.albumLabel.SetText(album)
		} else if artist != "" {
			p.artistLabel.SetText(artist)
			p.albumLabel.SetText("")
		} else {
			p.artistLabel.SetText(album)
			p.albumLabel.SetText("")
		}
	} else {
		p.titleLabel.SetText("No Track")
		p.artistLabel.SetText("")
		p.albumLabel.SetText("")
	}

	// Playback state
	if state.Playback != nil {
		pb := state.Playback

		// Seek bar — skip if user is dragging
		if !p.seeking {
			p.seekBar.SetRange(0, float64(pb.DurationMS))
			p.seekBar.SetValue(float64(pb.PositionMS))
		}
		p.posLabel.SetText(formatDuration(pb.PositionMS))
		p.durLabel.SetText(formatDuration(pb.DurationMS))

		// Play/pause icon
		if pb.Status != p.lastStatus {
			p.lastStatus = pb.Status
			switch pb.Status {
			case "playing":
				setButtonIcon(p.playBtn, "media-playback-pause")
			default:
				setButtonIcon(p.playBtn, "media-playback-start")
			}
		}

		// Status label
		p.statusLabel.SetText(pb.Status)

		// Enable/disable controls based on status
		active := pb.Status == "playing" || pb.Status == "paused"
		p.prevBtn.SetSensitive(active)
		p.nextBtn.SetSensitive(active)
		p.seekBar.SetSensitive(active)

		// Volume
		p.volumeBar.SetValue(pb.Volume)
		p.volumeLabel.SetText(fmt.Sprintf("%d%%", int(pb.Volume*100)))
	}

	// Queue summary
	p.updateQueue(state)
}

func (p *Popup) updateQueue(state *mu.RendererState) {
	// Clear existing queue children
	if children := p.queueBox.GetChildren(); children != nil {
		children.Foreach(func(item interface{}) {
			if w, ok := item.(*gtk.Widget); ok {
				p.queueBox.Remove(w)
			}
		})
	}

	if state.Queue == nil {
		return
	}
	q := state.Queue
	idx := int(q.Index)

	// Header
	hdr, _ := gtk.LabelNew(fmt.Sprintf("Queue (%d tracks)", q.Length))
	sc, _ := hdr.GetStyleContext()
	sc.AddClass("queue-header")
	hdr.SetHAlign(gtk.ALIGN_START)
	p.queueBox.PackStart(hdr, false, false, 2)

	// Show prev / current / next from cached queue items
	if len(p.queueItems) > 0 {
		// Previous track
		if idx > 0 && idx-1 < len(p.queueItems) {
			p.addQueueLabel("  "+queueItemName(p.queueItems[idx-1]), "queue-more")
		}
		// Current track
		if idx < len(p.queueItems) {
			p.addQueueLabel("▶ "+queueItemName(p.queueItems[idx]), "queue-current")
		}
		// Next tracks (up to 3)
		shown := 0
		for i := idx + 1; i < len(p.queueItems) && shown < 3; i++ {
			p.addQueueLabel("  "+queueItemName(p.queueItems[i]), "queue-more")
			shown++
		}
		remaining := int(q.Length) - idx - 1 - shown
		if remaining > 0 {
			p.addQueueLabel(fmt.Sprintf("  ... +%d more", remaining), "queue-more")
		}
	} else {
		// No cached queue — show count from state
		if state.Current != nil {
			title := metaString(state.Current.Metadata, "title", "")
			if title == "" {
				title = displayName(state.Current.ItemID)
			}
			p.addQueueLabel("▶ "+title, "queue-current")
		}
		remaining := int(q.Length) - idx - 1
		if remaining > 0 {
			p.addQueueLabel(fmt.Sprintf("  +%d more", remaining), "queue-more")
		}
	}

	p.queueBox.ShowAll()
}

func (p *Popup) addQueueLabel(text, cssClass string) {
	lbl, err := gtk.LabelNew(text)
	if err != nil {
		return
	}
	sc, _ := lbl.GetStyleContext()
	sc.AddClass(cssClass)
	lbl.SetHAlign(gtk.ALIGN_START)
	lbl.SetEllipsize(3)
	lbl.SetMaxWidthChars(40)
	p.queueBox.PackStart(lbl, false, false, 0)
}

func queueItemName(item mu.QueueItem) string {
	title := metaString(item.Metadata, "title", "")
	artist := metaString(item.Metadata, "artist", "")
	if title != "" && artist != "" {
		return fmt.Sprintf("%s — %s", title, artist)
	}
	if title != "" {
		return title
	}
	return displayName(item.ItemID)
}

func (p *Popup) buildUI() error {
	// Apply CSS styling
	if err := p.applyCSS(); err != nil {
		return err
	}

	// Main container
	mainBox, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	if err != nil {
		return err
	}
	sc, err := mainBox.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("popup-main")

	// Header box — track info
	p.headerBox, err = gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	if err != nil {
		return err
	}
	sc, err = p.headerBox.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("popup-header")
	p.headerBox.SetMarginStart(12)
	p.headerBox.SetMarginEnd(12)
	p.headerBox.SetMarginTop(10)
	p.headerBox.SetMarginBottom(6)

	p.titleLabel, err = gtk.LabelNew("No Track")
	if err != nil {
		return err
	}
	sc, err = p.titleLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("track-title")
	p.titleLabel.SetHAlign(gtk.ALIGN_START)
	p.titleLabel.SetEllipsize(3)
	p.titleLabel.SetMaxWidthChars(38)

	p.artistLabel, err = gtk.LabelNew("")
	if err != nil {
		return err
	}
	sc, err = p.artistLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("track-artist")
	p.artistLabel.SetHAlign(gtk.ALIGN_START)
	p.artistLabel.SetEllipsize(3)
	p.artistLabel.SetMaxWidthChars(38)

	p.albumLabel, err = gtk.LabelNew("")
	if err != nil {
		return err
	}
	sc, err = p.albumLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("track-album")
	p.albumLabel.SetHAlign(gtk.ALIGN_START)
	p.albumLabel.SetEllipsize(3)

	p.statusLabel, err = gtk.LabelNew("")
	if err != nil {
		return err
	}
	sc, err = p.statusLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("track-status")
	p.statusLabel.SetHAlign(gtk.ALIGN_END)

	p.headerBox.PackStart(p.titleLabel, false, false, 0)
	p.headerBox.PackStart(p.artistLabel, false, false, 0)
	p.headerBox.PackStart(p.albumLabel, false, false, 0)
	p.headerBox.PackStart(p.statusLabel, false, false, 0)
	mainBox.PackStart(p.headerBox, false, false, 0)

	// Seek bar
	seekBox, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	if err != nil {
		return err
	}
	seekBox.SetMarginStart(12)
	seekBox.SetMarginEnd(12)

	p.seekBar, err = gtk.ScaleNewWithRange(gtk.ORIENTATION_HORIZONTAL, 0, 100, 1)
	if err != nil {
		return err
	}
	p.seekBar.SetDrawValue(false)
	sc, err = p.seekBar.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("seek-bar")

	p.seekBar.Connect("button-press-event", func() bool {
		p.seeking = true
		return false
	})
	p.seekBar.Connect("button-release-event", func() bool {
		p.seeking = false
		val := p.seekBar.GetValue()
		p.sendCmd("playback.seek", mu.PlaybackSeekBody{
			PositionMS: int64(val),
		})
		return false
	})

	seekLabels, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	if err != nil {
		return err
	}
	p.posLabel, err = gtk.LabelNew("0:00")
	if err != nil {
		return err
	}
	sc, err = p.posLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("seek-time")
	p.posLabel.SetHAlign(gtk.ALIGN_START)

	p.durLabel, err = gtk.LabelNew("0:00")
	if err != nil {
		return err
	}
	sc, err = p.durLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("seek-time")
	p.durLabel.SetHAlign(gtk.ALIGN_END)

	seekLabels.PackStart(p.posLabel, true, true, 0)
	seekLabels.PackEnd(p.durLabel, true, true, 0)

	seekBox.PackStart(p.seekBar, false, false, 0)
	seekBox.PackStart(seekLabels, false, false, 0)
	mainBox.PackStart(seekBox, false, false, 4)

	// Transport controls
	transport, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
	if err != nil {
		return err
	}
	transport.SetHAlign(gtk.ALIGN_CENTER)
	transport.SetMarginTop(4)
	transport.SetMarginBottom(4)

	p.prevBtn, err = gtk.ButtonNewFromIconName("media-skip-backward", gtk.ICON_SIZE_BUTTON)
	if err != nil {
		return err
	}
	sc, err = p.prevBtn.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("transport-btn")
	p.prevBtn.Connect("clicked", func() {
		p.sendCmd("playback.prev", nil)
	})

	p.playBtn, err = gtk.ButtonNewFromIconName("media-playback-start", gtk.ICON_SIZE_BUTTON)
	if err != nil {
		return err
	}
	sc, err = p.playBtn.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("transport-btn")
	sc.AddClass("play-btn")
	p.playBtn.Connect("clicked", func() {
		if p.lastStatus == "playing" {
			p.sendCmd("playback.pause", nil)
		} else {
			p.sendCmd("playback.play", mu.PlaybackPlayBody{})
		}
	})

	p.nextBtn, err = gtk.ButtonNewFromIconName("media-skip-forward", gtk.ICON_SIZE_BUTTON)
	if err != nil {
		return err
	}
	sc, err = p.nextBtn.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("transport-btn")
	p.nextBtn.Connect("clicked", func() {
		p.sendCmd("playback.next", nil)
	})

	transport.PackStart(p.prevBtn, false, false, 0)
	transport.PackStart(p.playBtn, false, false, 0)
	transport.PackStart(p.nextBtn, false, false, 0)
	mainBox.PackStart(transport, false, false, 0)

	// Volume
	volBox, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	if err != nil {
		return err
	}
	volBox.SetMarginStart(12)
	volBox.SetMarginEnd(12)
	volBox.SetMarginTop(4)
	volBox.SetMarginBottom(4)

	volIcon, err := gtk.LabelNew("\U0001F509") // speaker icon
	if err != nil {
		return err
	}
	sc, err = volIcon.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("volume-icon")

	p.volumeBar, err = gtk.ScaleNewWithRange(gtk.ORIENTATION_HORIZONTAL, 0.0, 1.0, 0.01)
	if err != nil {
		return err
	}
	p.volumeBar.SetDrawValue(false)
	sc, err = p.volumeBar.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("volume-bar")
	p.volumeBar.Connect("value-changed", func() {
		vol := p.volumeBar.GetValue()
		p.sendCmd("playback.setVolume", mu.PlaybackSetVolumeBody{
			Volume: vol,
		})
		p.volumeLabel.SetText(fmt.Sprintf("%d%%", int(vol*100)))
	})

	p.volumeLabel, err = gtk.LabelNew("0%")
	if err != nil {
		return err
	}
	sc, err = p.volumeLabel.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("volume-pct")

	volBox.PackStart(volIcon, false, false, 0)
	volBox.PackStart(p.volumeBar, true, true, 0)
	volBox.PackEnd(p.volumeLabel, false, false, 0)
	mainBox.PackStart(volBox, false, false, 0)

	// Separator
	sep, err := gtk.SeparatorNew(gtk.ORIENTATION_HORIZONTAL)
	if err != nil {
		return err
	}
	sc, err = sep.GetStyleContext()
	if err != nil {
		return err
	}
	sc.AddClass("popup-sep")
	mainBox.PackStart(sep, false, false, 4)

	// Queue — scrolled window
	scrollWin, err := gtk.ScrolledWindowNew(nil, nil)
	if err != nil {
		return err
	}
	scrollWin.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	scrollWin.SetMinContentHeight(40)
	scrollWin.SetSizeRequest(-1, 80)

	p.queueBox, err = gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	if err != nil {
		return err
	}
	p.queueBox.SetMarginStart(12)
	p.queueBox.SetMarginEnd(12)
	p.queueBox.SetMarginBottom(8)
	scrollWin.Add(p.queueBox)
	mainBox.PackStart(scrollWin, true, true, 0)

	// Lease control bar
	sep2, err := gtk.SeparatorNew(gtk.ORIENTATION_HORIZONTAL)
	if err != nil {
		return err
	}
	sc, _ = sep2.GetStyleContext()
	sc.AddClass("popup-sep")
	mainBox.PackStart(sep2, false, false, 2)

	leaseBox, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	if err != nil {
		return err
	}
	leaseBox.SetMarginStart(12)
	leaseBox.SetMarginEnd(12)
	leaseBox.SetMarginBottom(6)
	leaseBox.SetMarginTop(2)

	p.leaseLabel, err = gtk.LabelNew("● Control: active")
	if err != nil {
		return err
	}
	sc, _ = p.leaseLabel.GetStyleContext()
	sc.AddClass("lease-active")
	p.leaseLabel.SetHAlign(gtk.ALIGN_START)

	p.leaseBtn, err = gtk.ButtonNewWithLabel("Release")
	if err != nil {
		return err
	}
	sc, _ = p.leaseBtn.GetStyleContext()
	sc.AddClass("lease-btn")
	p.leaseBtn.Connect("clicked", func() {
		if p.hasLease {
			if p.onLeaseRelease != nil {
				p.onLeaseRelease()
			}
		} else {
			if p.onLeaseAcquire != nil {
				p.onLeaseAcquire()
			}
		}
	})

	leaseBox.PackStart(p.leaseLabel, true, true, 0)
	leaseBox.PackEnd(p.leaseBtn, false, false, 0)
	mainBox.PackStart(leaseBox, false, false, 0)

	p.win.Add(mainBox)

	// Initialize all widgets but start hidden
	p.win.ShowAll()
	p.win.Hide()

	return nil
}

func (p *Popup) applyCSS() error {
	css, err := gtk.CssProviderNew()
	if err != nil {
		return err
	}

	err = css.LoadFromData(`
		.popup-main {
			background-color: #1a1a2e;
			padding: 0;
		}
		.popup-header {
			background-color: #0f3460;
			padding: 8px;
		}
		.track-title {
			color: #ffffff;
			font-size: 14px;
			font-weight: bold;
		}
		.track-artist {
			color: #a0a0b0;
			font-size: 12px;
		}
		.track-album {
			color: #707080;
			font-size: 11px;
		}
		.track-status {
			color: #707080;
			font-size: 10px;
		}
		.seek-time {
			color: #a0a0b0;
			font-size: 10px;
		}
		button {
			background-color: #2a2a4e;
			background-image: none;
			border: none;
			box-shadow: none;
			color: #e0e0e0;
		}
		.transport-btn {
			background-color: transparent;
			min-width: 36px;
			min-height: 36px;
		}
		.transport-btn:hover {
			background-color: #2a2a4e;
		}
		.play-btn {
			background-color: #e94560;
		}
		.play-btn:hover {
			background-color: #ff6b81;
		}
		.volume-icon {
			color: #a0a0b0;
			font-size: 14px;
		}
		scale trough {
			background-color: #2a2a4e;
			min-height: 6px;
		}
		scale highlight {
			background-color: #4ecca3;
			min-height: 6px;
		}
		scale slider {
			background-color: #e0e0e0;
			min-width: 12px;
			min-height: 12px;
		}
		.volume-pct {
			color: #a0a0b0;
			font-size: 10px;
			min-width: 36px;
		}
		.popup-sep {
			background-color: #2a2a4e;
			min-height: 1px;
		}
		.queue-header {
			color: #a0a0b0;
			font-size: 11px;
			font-weight: bold;
		}
		.queue-current {
			color: #e94560;
			font-size: 11px;
		}
		.queue-more {
			color: #707080;
			font-size: 10px;
		}
		.lease-active {
			color: #4ecca3;
			font-size: 10px;
		}
		.lease-inactive {
			color: #e94560;
			font-size: 10px;
		}
		.lease-btn {
			background-color: #2a2a4e;
			color: #a0a0b0;
			border: none;
			font-size: 10px;
			padding: 2px 8px;
		}
	`)
	if err != nil {
		return err
	}

	screen, err := gdk.ScreenGetDefault()
	if err != nil {
		return err
	}
	gtk.AddProviderForScreen(screen, css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)

	return nil
}

// metaString extracts a string from metadata with a fallback.
func metaString(md map[string]interface{}, key, fallback string) string {
	if md == nil {
		return fallback
	}
	if v, ok := md[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

// displayName extracts a human-readable name from a URL or item ID.
func displayName(itemID string) string {
	if itemID == "" {
		return "No Track"
	}
	if u, err := url.Parse(itemID); err == nil && u.Path != "" {
		base := path.Base(u.Path)
		// Strip file extension
		if i := strings.LastIndex(base, "."); i > 0 {
			base = base[:i]
		}
		// URL-decode
		if decoded, err := url.PathUnescape(base); err == nil {
			return decoded
		}
		return base
	}
	return itemID
}

// setButtonIcon replaces the image on a button.
func setButtonIcon(btn *gtk.Button, iconName string) {
	img, err := gtk.ImageNewFromIconName(iconName, gtk.ICON_SIZE_BUTTON)
	if err == nil {
		btn.SetImage(img)
	}
}

// formatDuration formats milliseconds as "M:SS".
func formatDuration(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSec := ms / 1000
	min := totalSec / 60
	sec := totalSec % 60
	return fmt.Sprintf("%d:%02d", min, sec)
}
