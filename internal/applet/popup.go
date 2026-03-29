//go:build gtk

package applet

import (
	"fmt"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/gtk"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// CommandFunc sends a command via MQTT.
type CommandFunc func(cmdType string, body interface{})

// Popup is the mini-player popup window.
type Popup struct {
	win *gtk.Window

	// Widgets
	titleLabel  *gtk.Label
	artistLabel *gtk.Label
	albumLabel  *gtk.Label
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

	sendCmd CommandFunc

	// State tracking to avoid redundant updates
	seeking    bool
	lastStatus string
}

// NewPopup creates the mini-player popup window.
func NewPopup(sendCmd CommandFunc) (*Popup, error) {
	win, err := gtk.WindowNew(gtk.WINDOW_POPUP)
	if err != nil {
		return nil, err
	}
	win.SetDefaultSize(300, -1)
	win.SetResizable(false)
	win.SetDecorated(false)
	win.SetSkipTaskbarHint(true)
	win.SetSkipPagerHint(true)
	win.SetTypeHint(gdk.WINDOW_TYPE_HINT_POPUP_MENU)

	p := &Popup{win: win, sendCmd: sendCmd}
	if err := p.buildUI(); err != nil {
		return nil, err
	}

	// Dismiss on focus loss
	win.Connect("focus-out-event", func() bool {
		p.Hide()
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

// ShowCentered shows the popup in the center of the default screen.
func (p *Popup) ShowCentered() {
	screen, err := gdk.ScreenGetDefault()
	if err != nil {
		p.win.ShowAll()
		return
	}
	sw := screen.GetWidth()
	sh := screen.GetHeight()

	_, pw := p.win.GetPreferredWidth()
	ph := p.win.GetAllocatedHeight()
	if ph <= 1 {
		ph = 400 // estimate before first show
	}
	popupX := (sw - pw) / 2
	popupY := (sh - ph) / 2

	p.win.Move(popupX, popupY)
	p.win.ShowAll()
	p.win.GrabFocus()
}

// UpdateState refreshes all widgets from the renderer state.
func (p *Popup) UpdateState(state *mu.RendererState) {
	if state == nil {
		return
	}

	// Track info
	if state.Current != nil && state.Current.Metadata != nil {
		md := state.Current.Metadata
		p.titleLabel.SetText(metaString(md, "title", "No Track"))
		p.artistLabel.SetText(metaString(md, "artist", ""))
		p.albumLabel.SetText(metaString(md, "album", ""))
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

	// Header: track count
	hdr, err := gtk.LabelNew(fmt.Sprintf("Queue (%d tracks)", q.Length))
	if err == nil {
		sc, err := hdr.GetStyleContext()
		if err == nil {
			sc.AddClass("queue-header")
		}
		hdr.SetHAlign(gtk.ALIGN_START)
		p.queueBox.PackStart(hdr, false, false, 2)
	}

	// Current track highlight
	if state.Current != nil && state.Current.Metadata != nil {
		name := metaString(state.Current.Metadata, "title", state.Current.ItemID)
		cur, err := gtk.LabelNew(fmt.Sprintf("▶ %s", name))
		if err == nil {
			sc, err := cur.GetStyleContext()
			if err == nil {
				sc.AddClass("queue-current")
			}
			cur.SetHAlign(gtk.ALIGN_START)
			cur.SetEllipsize(3) // PANGO_ELLIPSIZE_END
			p.queueBox.PackStart(cur, false, false, 0)
		}
	}

	// Remaining tracks
	remaining := q.Length - q.Index - 1
	if remaining > 0 {
		more, err := gtk.LabelNew(fmt.Sprintf("+%d more", remaining))
		if err == nil {
			sc, err := more.GetStyleContext()
			if err == nil {
				sc.AddClass("queue-more")
			}
			more.SetHAlign(gtk.ALIGN_START)
			p.queueBox.PackStart(more, false, false, 0)
		}
	}

	p.queueBox.ShowAll()
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
		p.sendCmd("playback.prev", struct{}{})
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
		p.sendCmd("playback.pause", struct{}{})
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
		p.sendCmd("playback.next", struct{}{})
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
	scrollWin.SetMinContentHeight(60)
	scrollWin.SetSizeRequest(-1, 200)

	p.queueBox, err = gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	if err != nil {
		return err
	}
	p.queueBox.SetMarginStart(12)
	p.queueBox.SetMarginEnd(12)
	p.queueBox.SetMarginBottom(8)
	scrollWin.Add(p.queueBox)
	mainBox.PackStart(scrollWin, true, true, 0)

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
			border-radius: 8px;
			padding: 0;
		}
		.popup-header {
			background: linear-gradient(to bottom, #16213e, #0f3460);
			border-radius: 8px 8px 0 0;
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
		.transport-btn {
			background: transparent;
			border: none;
			color: #ffffff;
			min-width: 36px;
			min-height: 36px;
		}
		.transport-btn:hover {
			background-color: rgba(255,255,255,0.1);
			border-radius: 18px;
		}
		.play-btn {
			background-color: #e94560;
			border-radius: 18px;
		}
		.play-btn:hover {
			background-color: #ff6b81;
		}
		.volume-icon {
			color: #a0a0b0;
			font-size: 14px;
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
	if v, ok := md[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
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
