//go:build gtk

package applet

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
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
	albumLabel  *gtk.Label
	artistLabel *gtk.Label
	artworkImg  *gtk.Image
	seekBar     *gtk.Scale
	posLabel    *gtk.Label
	durLabel    *gtk.Label
	playBtn     *gtk.Button
	prevBtn     *gtk.Button
	nextBtn     *gtk.Button
	stopBtn     *gtk.Button
	volumeBar   *gtk.Scale
	volumeLabel *gtk.Label
	queueBox    *gtk.Box
	leaseBtn     *gtk.Button
	leaseLabel   *gtk.Label
	bitrateLabel *gtk.Label
	playlistCombo *gtk.ComboBoxText

	sendCmd          CommandFunc
	onLeaseAcquire   LeaseFunc
	onLeaseRelease   LeaseFunc
	onLoadPlaylist   func(playlistID string)

	// State tracking
	seeking    bool
	lastStatus string
	hasLease   bool
	lastState  *mu.RendererState

	// Queue cache
	queueItems []mu.QueueItem
	queueIndex int64

	// Artwork
	artMu         sync.Mutex
	lastArtworkID string

	// Notifications
	lastItemID    string
	pendingNotify bool
}

// NewPopup creates the mini-player popup window.
func NewPopup(sendCmd CommandFunc, onLeaseAcquire, onLeaseRelease LeaseFunc, onLoadPlaylist func(string)) (*Popup, error) {
	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return nil, err
	}
	win.SetSizeRequest(340, -1)
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
		onLoadPlaylist: onLoadPlaylist,
		hasLease:       true,
	}
	if err := p.buildUI(); err != nil {
		return nil, err
	}

	win.Connect("key-press-event", func(_ *gtk.Window, ev *gdk.Event) bool {
		keyEvent := gdk.EventKeyNewFromEvent(ev)
		if keyEvent.KeyVal() == 0xff1b {
			p.Hide()
			return true
		}
		return false
	})

	return p, nil
}

func (p *Popup) IsVisible() bool { return p.win.IsVisible() }
func (p *Popup) Hide()           { p.win.Hide() }

func (p *Popup) ShowAt(x, y, width, height int) {
	_, pw := p.win.GetPreferredWidth()
	p.win.Move(x+(width-pw)/2, y-10)
	p.win.ShowAll()
	p.win.GrabFocus()
}

func (p *Popup) ShowCentered() {
	p.win.ShowAll()
	alloc := p.win.GetAllocation()
	pw := alloc.GetWidth()
	if pw <= 1 {
		pw = 340
	}
	screen, err := gdk.ScreenGetDefault()
	if err != nil {
		return
	}
	p.win.Move(screen.GetWidth()-pw-4, 24)
	p.win.GrabFocus()
}

// SetPlaylists populates the playlist dropdown.
func (p *Popup) SetPlaylists(playlists []mu.PlaylistSummary) {
	p.playlistCombo.RemoveAll()
	p.playlistCombo.AppendText("Load playlist...")
	for _, pl := range playlists {
		p.playlistCombo.Append(pl.PlaylistID, pl.Name)
	}
	p.playlistCombo.SetActive(0)
}

// SetQueueItems updates the cached queue and re-renders track info.
func (p *Popup) SetQueueItems(items []mu.QueueItem, index int64) {
	p.queueItems = items
	p.queueIndex = index
	// Reset artwork cache so it re-fetches with new metadata
	p.artMu.Lock()
	p.lastArtworkID = ""
	p.artMu.Unlock()
	// Re-render with cached state now that we have queue metadata
	if p.lastState != nil {
		p.UpdateState(p.lastState)
	}
}

func (p *Popup) SetHasLease(has bool) {
	p.hasLease = has
	if has {
		p.leaseLabel.SetText("● CTRL")
		sc, _ := p.leaseLabel.GetStyleContext()
		sc.RemoveClass("lease-off")
		sc.AddClass("lease-on")
		p.leaseBtn.SetLabel("Release")
	} else {
		p.leaseLabel.SetText("○ CTRL")
		sc, _ := p.leaseLabel.GetStyleContext()
		sc.RemoveClass("lease-on")
		sc.AddClass("lease-off")
		p.leaseBtn.SetLabel("Take")
	}
}

// UpdateState refreshes all widgets from renderer state.
func (p *Popup) UpdateState(state *mu.RendererState) {
	if state == nil {
		return
	}
	p.lastState = state

	// Resolve metadata: try Current.Metadata, fall back to queue cache
	var md map[string]interface{}
	if state.Current != nil {
		md = state.Current.Metadata
		if (md == nil || metaString(md, "title", "") == "") && len(p.queueItems) > 0 {
			idx := int(p.queueIndex)
			if idx >= 0 && idx < len(p.queueItems) {
				if p.queueItems[idx].ItemID == state.Current.ItemID {
					md = p.queueItems[idx].Metadata
				}
			}
		}
	}

	// Track change detection
	if state.Current != nil && state.Current.ItemID != p.lastItemID {
		if p.lastItemID != "" {
			p.pendingNotify = true
		}
		p.lastItemID = state.Current.ItemID
	}

	// Track info
	if state.Current != nil {
		title := metaString(md, "title", "")
		if title == "" {
			title = displayName(state.Current.ItemID)
		}
		artist := metaString(md, "artists", "")
		if artist == "" {
			artist = metaString(md, "artist", "")
		}
		album := metaString(md, "album", "")
		p.titleLabel.SetText(title)
		p.titleLabel.SetTooltipText(title)
		p.albumLabel.SetText(album)
		p.albumLabel.SetTooltipText(album)
		p.artistLabel.SetText(artist)
		p.artistLabel.SetTooltipText(artist)

		// Notify on track change when popup is hidden (wait for real metadata)
		if p.pendingNotify && !p.IsVisible() && title != "Track" && title != "Unknown" {
			p.pendingNotify = false
			notifBody := artist
			if album != "" && artist != "" {
				notifBody = artist + " — " + album
			}
			go notifyTrackChange(title, notifBody, metaString(md, "artworkUrl", ""))
		}

		// Artwork
		artURL := metaString(md, "artworkUrl", "")
		artKey := state.Current.ItemID
		p.artMu.Lock()
		needsFetch := artKey != p.lastArtworkID
		if needsFetch {
			p.lastArtworkID = artKey
		}
		p.artMu.Unlock()
		if needsFetch {
			if artURL != "" {
				go p.fetchArtwork(artURL)
			} else {
				p.setDefaultArtwork()
			}
		}
	} else {
		p.titleLabel.SetText("mu-applet")
		p.albumLabel.SetText("")
		p.artistLabel.SetText("No track loaded")
		p.setDefaultArtwork()
	}

	if state.Playback != nil {
		pb := state.Playback

		if !p.seeking {
			p.seekBar.SetRange(0, float64(pb.DurationMS))
			p.seekBar.SetValue(float64(pb.PositionMS))
		}
		p.posLabel.SetText(formatDuration(pb.PositionMS))
		p.durLabel.SetText(formatDuration(pb.DurationMS))

		if pb.Status != p.lastStatus {
			p.lastStatus = pb.Status
			if pb.Status == "playing" {
				setButtonIcon(p.playBtn, "media-playback-pause")
			} else {
				setButtonIcon(p.playBtn, "media-playback-start")
			}
		}

		active := pb.Status == "playing" || pb.Status == "paused"
		p.prevBtn.SetSensitive(active)
		p.nextBtn.SetSensitive(active)
		p.stopBtn.SetSensitive(active)
		p.seekBar.SetSensitive(active)

		p.volumeBar.SetValue(pb.Volume)
		p.volumeLabel.SetText(fmt.Sprintf("Vol %d%%", int(pb.Volume*100)))

		// Bitrate/status info
		kbps := metaString(md, "bitrate", "")
		sampleRate := metaString(md, "sampleRate", "")
		if kbps != "" {
			p.bitrateLabel.SetText(fmt.Sprintf("%skbps %s", kbps, sampleRate))
		} else {
			p.bitrateLabel.SetText(pb.Status)
		}
	}

	p.updateQueue(state)
}

func (p *Popup) updateQueue(state *mu.RendererState) {
	// Destroy old children explicitly to prevent GC/finalizer race with GTK
	if children := p.queueBox.GetChildren(); children != nil {
		children.Foreach(func(item interface{}) {
			if w, ok := item.(*gtk.Widget); ok {
				w.Destroy()
			}
		})
	}
	if state.Queue == nil {
		return
	}
	q := state.Queue
	idx := int(q.Index)

	if len(p.queueItems) > 0 {
		// Previous
		if idx > 0 && idx-1 < len(p.queueItems) {
			p.addQueueLabel("  "+queueItemName(p.queueItems[idx-1]), "q-dim", idx-1)
		}
		// Current
		if idx < len(p.queueItems) {
			p.addQueueLabel("▶ "+queueItemName(p.queueItems[idx]), "q-now", -1)
		}
		// Next 5
		shown := 0
		for i := idx + 1; i < len(p.queueItems) && shown < 5; i++ {
			p.addQueueLabel("  "+queueItemName(p.queueItems[i]), "q-next", i)
			shown++
		}
		rem := int(q.Length) - idx - 1 - shown
		if rem > 0 {
			p.addQueueLabel(fmt.Sprintf("  +%d more", rem), "q-dim", -1)
		}
	} else if state.Current != nil {
		title := metaString(state.Current.Metadata, "title", "")
		if title == "" {
			title = displayName(state.Current.ItemID)
		}
		p.addQueueLabel("▶ "+title, "q-now", -1)
		rem := int(q.Length) - idx - 1
		if rem > 0 {
			p.addQueueLabel(fmt.Sprintf("  +%d more", rem), "q-dim", -1)
		}
	}
	p.queueBox.ShowAll()
}

func (p *Popup) addQueueLabel(text, cssClass string, jumpIndex int) {
	tooltip := strings.TrimSpace(strings.TrimPrefix(text, "▶"))
	tooltip = strings.TrimSpace(tooltip)
	if jumpIndex >= 0 {
		btn, _ := gtk.ButtonNew()
		lbl, _ := gtk.LabelNew(text)
		sc, _ := lbl.GetStyleContext()
		sc.AddClass(cssClass)
		lbl.SetHAlign(gtk.ALIGN_START)
		lbl.SetEllipsize(3)
		lbl.SetMaxWidthChars(42)
		btn.Add(lbl)
		btn.SetTooltipText(tooltip)
		sc, _ = btn.GetStyleContext()
		sc.AddClass("q-btn")
		idx := int64(jumpIndex)
		btn.Connect("clicked", func() {
			p.sendCmd("queue.jump", mu.QueueJumpBody{Index: idx})
			p.sendCmd("playback.play", mu.PlaybackPlayBody{})
		})
		p.queueBox.PackStart(btn, false, false, 0)
	} else {
		lbl, _ := gtk.LabelNew(text)
		sc, _ := lbl.GetStyleContext()
		sc.AddClass(cssClass)
		lbl.SetHAlign(gtk.ALIGN_START)
		lbl.SetTooltipText(tooltip)
		lbl.SetEllipsize(3)
		lbl.SetMaxWidthChars(42)
		p.queueBox.PackStart(lbl, false, false, 0)
	}
}

func (p *Popup) buildUI() error {
	if err := p.applyCSS(); err != nil {
		return err
	}

	main, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	sc, _ := main.GetStyleContext()
	sc.AddClass("wa")

	// === Title bar ===
	titleBar, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	sc, _ = titleBar.GetStyleContext()
	sc.AddClass("wa-titlebar")
	titleBar.SetMarginStart(6)
	titleBar.SetMarginEnd(6)
	titleBar.SetMarginTop(4)
	titleBar.SetMarginBottom(2)

	appName, _ := gtk.LabelNew("mu-applet")
	sc, _ = appName.GetStyleContext()
	sc.AddClass("wa-appname")
	appName.SetHAlign(gtk.ALIGN_START)

	p.bitrateLabel, _ = gtk.LabelNew("stopped")
	sc, _ = p.bitrateLabel.GetStyleContext()
	sc.AddClass("wa-bitrate")

	titleBar.PackStart(appName, true, true, 0)
	titleBar.PackEnd(p.bitrateLabel, false, false, 0)
	main.PackStart(titleBar, false, false, 0)

	// === Display area: artwork + track info ===
	display, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	display.SetMarginStart(6)
	display.SetMarginEnd(6)
	display.SetMarginTop(2)
	display.SetMarginBottom(4)

	p.artworkImg, _ = gtk.ImageNewFromIconName("audio-x-generic", gtk.ICON_SIZE_DIALOG)
	p.artworkImg.SetSizeRequest(56, 56)
	display.PackStart(p.artworkImg, false, false, 0)

	info, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 1)
	p.titleLabel, _ = gtk.LabelNew("mu-applet")
	sc, _ = p.titleLabel.GetStyleContext()
	sc.AddClass("wa-title")
	p.titleLabel.SetHAlign(gtk.ALIGN_START)
	p.titleLabel.SetEllipsize(3)
	p.titleLabel.SetMaxWidthChars(30)

	p.albumLabel, _ = gtk.LabelNew("")
	sc, _ = p.albumLabel.GetStyleContext()
	sc.AddClass("wa-album")
	p.albumLabel.SetHAlign(gtk.ALIGN_START)
	p.albumLabel.SetEllipsize(3)
	p.albumLabel.SetMaxWidthChars(30)

	p.artistLabel, _ = gtk.LabelNew("")
	sc, _ = p.artistLabel.GetStyleContext()
	sc.AddClass("wa-artist")
	p.artistLabel.SetHAlign(gtk.ALIGN_START)
	p.artistLabel.SetEllipsize(3)
	p.artistLabel.SetMaxWidthChars(30)

	info.PackStart(p.titleLabel, false, false, 0)
	info.PackStart(p.albumLabel, false, false, 0)
	info.PackStart(p.artistLabel, false, false, 0)
	display.PackStart(info, true, true, 0)
	main.PackStart(display, false, false, 0)

	// === Seek ===
	seekRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 4)
	seekRow.SetMarginStart(6)
	seekRow.SetMarginEnd(6)

	p.posLabel, _ = gtk.LabelNew("0:00")
	sc, _ = p.posLabel.GetStyleContext()
	sc.AddClass("wa-time")

	p.seekBar, _ = gtk.ScaleNewWithRange(gtk.ORIENTATION_HORIZONTAL, 0, 100, 1)
	p.seekBar.SetDrawValue(false)
	p.seekBar.SetCanFocus(false)
	p.seekBar.Connect("button-press-event", func() bool { p.seeking = true; return false })
	p.seekBar.Connect("button-release-event", func() bool {
		p.seeking = false
		p.sendCmd("playback.seek", mu.PlaybackSeekBody{PositionMS: int64(p.seekBar.GetValue())})
		return false
	})

	p.durLabel, _ = gtk.LabelNew("0:00")
	sc, _ = p.durLabel.GetStyleContext()
	sc.AddClass("wa-time")

	seekRow.PackStart(p.posLabel, false, false, 0)
	seekRow.PackStart(p.seekBar, true, true, 0)
	seekRow.PackEnd(p.durLabel, false, false, 0)
	main.PackStart(seekRow, false, false, 2)

	// === Transport ===
	transport, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 2)
	transport.SetHAlign(gtk.ALIGN_CENTER)
	transport.SetMarginTop(2)
	transport.SetMarginBottom(2)

	p.prevBtn, _ = gtk.ButtonNewFromIconName("media-skip-backward", gtk.ICON_SIZE_SMALL_TOOLBAR)
	sc, _ = p.prevBtn.GetStyleContext()
	sc.AddClass("wa-btn")
	p.prevBtn.Connect("clicked", func() { p.sendCmd("playback.prev", nil) })

	p.playBtn, _ = gtk.ButtonNewFromIconName("media-playback-start", gtk.ICON_SIZE_SMALL_TOOLBAR)
	sc, _ = p.playBtn.GetStyleContext()
	sc.AddClass("wa-btn")
	sc.AddClass("wa-play")
	p.playBtn.Connect("clicked", func() {
		if p.lastStatus == "playing" {
			p.sendCmd("playback.pause", nil)
		} else {
			p.sendCmd("playback.play", mu.PlaybackPlayBody{})
		}
	})

	p.stopBtn, _ = gtk.ButtonNewFromIconName("media-playback-stop", gtk.ICON_SIZE_SMALL_TOOLBAR)
	sc, _ = p.stopBtn.GetStyleContext()
	sc.AddClass("wa-btn")
	p.stopBtn.Connect("clicked", func() { p.sendCmd("playback.stop", nil) })

	p.nextBtn, _ = gtk.ButtonNewFromIconName("media-skip-forward", gtk.ICON_SIZE_SMALL_TOOLBAR)
	sc, _ = p.nextBtn.GetStyleContext()
	sc.AddClass("wa-btn")
	p.nextBtn.Connect("clicked", func() { p.sendCmd("playback.next", nil) })

	transport.PackStart(p.prevBtn, false, false, 0)
	transport.PackStart(p.playBtn, false, false, 0)
	transport.PackStart(p.stopBtn, false, false, 0)
	transport.PackStart(p.nextBtn, false, false, 0)
	main.PackStart(transport, false, false, 0)

	// === Volume ===
	volRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 4)
	volRow.SetMarginStart(6)
	volRow.SetMarginEnd(6)

	p.volumeLabel, _ = gtk.LabelNew("Vol 100%")
	sc, _ = p.volumeLabel.GetStyleContext()
	sc.AddClass("wa-vol-label")

	p.volumeBar, _ = gtk.ScaleNewWithRange(gtk.ORIENTATION_HORIZONTAL, 0, 1, 0.01)
	p.volumeBar.SetDrawValue(false)
	p.volumeBar.SetCanFocus(false)
	p.volumeBar.SetValue(1.0)
	p.volumeBar.Connect("value-changed", func() {
		vol := p.volumeBar.GetValue()
		p.sendCmd("playback.setVolume", mu.PlaybackSetVolumeBody{Volume: vol})
		p.volumeLabel.SetText(fmt.Sprintf("Vol %d%%", int(vol*100)))
	})

	volRow.PackStart(p.volumeLabel, false, false, 0)
	volRow.PackStart(p.volumeBar, true, true, 0)
	main.PackStart(volRow, false, false, 2)

	// === Queue ===
	sep, _ := gtk.SeparatorNew(gtk.ORIENTATION_HORIZONTAL)
	sc, _ = sep.GetStyleContext()
	sc.AddClass("wa-sep")
	main.PackStart(sep, false, false, 2)

	scrollWin, _ := gtk.ScrolledWindowNew(nil, nil)
	scrollWin.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	scrollWin.SetMinContentHeight(30)
	scrollWin.SetSizeRequest(-1, 100)

	p.queueBox, _ = gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 1)
	p.queueBox.SetMarginStart(6)
	p.queueBox.SetMarginEnd(6)
	p.queueBox.SetMarginTop(2)
	p.queueBox.SetMarginBottom(2)
	scrollWin.Add(p.queueBox)
	main.PackStart(scrollWin, true, true, 0)

	// === Lease bar ===
	sep2, _ := gtk.SeparatorNew(gtk.ORIENTATION_HORIZONTAL)
	sc, _ = sep2.GetStyleContext()
	sc.AddClass("wa-sep")
	main.PackStart(sep2, false, false, 1)

	leaseRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 4)
	leaseRow.SetMarginStart(6)
	leaseRow.SetMarginEnd(6)
	leaseRow.SetMarginBottom(4)
	leaseRow.SetMarginTop(2)

	p.leaseLabel, _ = gtk.LabelNew("● CTRL")
	sc, _ = p.leaseLabel.GetStyleContext()
	sc.AddClass("lease-on")
	p.leaseLabel.SetHAlign(gtk.ALIGN_START)

	p.leaseBtn, _ = gtk.ButtonNewWithLabel("Release")
	sc, _ = p.leaseBtn.GetStyleContext()
	sc.AddClass("wa-btn-sm")
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

	p.playlistCombo, _ = gtk.ComboBoxTextNew()
	sc, _ = p.playlistCombo.GetStyleContext()
	sc.AddClass("wa-btn-sm")
	p.playlistCombo.AppendText("Load playlist...")
	p.playlistCombo.SetActive(0)
	p.playlistCombo.Connect("changed", func() {
		idx := p.playlistCombo.GetActive()
		if idx <= 0 {
			return
		}
		id := p.playlistCombo.GetActiveID()
		if id != "" && p.onLoadPlaylist != nil {
			p.onLoadPlaylist(id)
		}
		p.playlistCombo.SetActive(0) // reset to "Load playlist..."
	})

	leaseRow.PackStart(p.leaseLabel, false, false, 0)
	leaseRow.PackStart(p.playlistCombo, true, true, 4)
	leaseRow.PackEnd(p.leaseBtn, false, false, 0)
	main.PackStart(leaseRow, false, false, 0)

	p.win.Add(main)
	p.win.ShowAll()
	p.win.Hide()
	return nil
}

func (p *Popup) applyCSS() error {
	css, _ := gtk.CssProviderNew()
	err := css.LoadFromData(`
		.wa {
			background-color: #0a0e1a;
			border: 1px solid #1a2040;
		}
		.wa-titlebar {
			background-color: #060a14;
			border-bottom: 1px solid #1a2040;
		}
		.wa-appname {
			color: #6688cc;
			font-size: 10px;
			font-weight: bold;
			font-family: monospace;
		}
		.wa-bitrate {
			color: #ddaa00;
			font-size: 9px;
			font-family: monospace;
		}
		.wa-title {
			color: #44ff66;
			font-size: 13px;
			font-weight: bold;
			font-family: monospace;
		}
		.wa-album {
			color: #88bbff;
			font-size: 10px;
			font-family: monospace;
		}
		.wa-artist {
			color: #ddaa00;
			font-size: 10px;
			font-family: monospace;
		}
		.wa-time {
			color: #ddaa00;
			font-size: 10px;
			font-weight: bold;
			font-family: monospace;
			min-width: 32px;
		}
		* {
			-gtk-outline-radius: 0;
			outline-style: none;
			outline-width: 0;
			outline-color: transparent;
		}
		*:focus, *:active, *:checked {
			outline-style: none;
			outline-width: 0;
			outline-color: transparent;
			box-shadow: none;
		}
		scale trough {
			background-color: #0a0e1a;
			border: 1px solid #1a2040;
			min-height: 6px;
		}
		scale highlight {
			background-color: #44ff66;
			min-height: 6px;
		}
		scale slider {
			background-color: #cccccc;
			border: 1px solid #555555;
			min-width: 10px;
			min-height: 10px;
		}
		scale:focus trough {
			border-color: #1a2040;
			outline-style: none;
			outline-color: transparent;
			box-shadow: none;
		}
		scale:focus slider {
			border-color: #555555;
			outline-style: none;
			outline-color: transparent;
			box-shadow: none;
		}
		scale:focus {
			outline-style: none;
			outline-color: transparent;
			outline-width: 0;
			border-color: transparent;
			box-shadow: none;
			-gtk-outline-radius: 0;
		}
		button {
			background-color: #151a2a;
			background-image: none;
			border: 1px solid #2a3050;
			box-shadow: none;
			color: #aabbdd;
			padding: 2px 4px;
		}
		button:hover {
			background-color: #1a2240;
			border-color: #44ff66;
		}
		.wa-btn {
			min-width: 28px;
			min-height: 28px;
			padding: 2px;
		}
		.wa-play {
			border-color: #44ff66;
		}
		.wa-btn-sm {
			font-size: 9px;
			font-family: monospace;
			padding: 1px 6px;
			min-height: 20px;
		}
		.wa-vol-label {
			color: #ddaa00;
			font-size: 9px;
			font-family: monospace;
			min-width: 54px;
		}
		.wa-sep {
			background-color: #1a2040;
			min-height: 1px;
		}
		.q-now {
			color: #ffffff;
			background-color: #1a2a50;
			font-size: 10px;
			font-family: monospace;
			padding: 1px 4px;
		}
		.q-next {
			color: #44ff66;
			font-size: 10px;
			font-family: monospace;
			padding: 1px 4px;
		}
		.q-dim {
			color: #4a5070;
			font-size: 10px;
			font-family: monospace;
			padding: 1px 4px;
		}
		.q-btn {
			background-color: transparent;
			border: none;
			padding: 0;
			min-height: 0;
		}
		.q-btn:hover {
			background-color: #1a2a50;
		}
		combobox button {
			background-color: #151a2a;
			background-image: none;
			border: 1px solid #2a3050;
			color: #88bbff;
			font-size: 9px;
			font-family: monospace;
			padding: 1px 4px;
			min-height: 20px;
		}
		combobox button:hover {
			background-color: #1a2240;
			border-color: #44ff66;
		}
		.lease-on {
			color: #44ff66;
			font-size: 9px;
			font-family: monospace;
		}
		.lease-off {
			color: #ff4444;
			font-size: 9px;
			font-family: monospace;
		}
	`)
	if err != nil {
		return err
	}
	screen, _ := gdk.ScreenGetDefault()
	gtk.AddProviderForScreen(screen, css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	return nil
}

// --- helpers ---

func queueItemName(item mu.QueueItem) string {
	title := metaString(item.Metadata, "title", "")
	artist := metaString(item.Metadata, "artists", "")
	if artist == "" {
		artist = metaString(item.Metadata, "artist", "")
	}
	if title != "" && artist != "" {
		return artist + " — " + title
	}
	if title != "" {
		return title
	}
	return displayName(item.ItemID)
}

func metaString(md map[string]interface{}, key, fallback string) string {
	if md == nil {
		return fallback
	}
	v, ok := md[key]
	if !ok {
		return fallback
	}
	switch val := v.(type) {
	case string:
		if val != "" {
			return val
		}
	case []interface{}:
		// Handle arrays like "artists": ["Name1", "Name2"]
		parts := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
	}
	return fallback
}

func displayName(itemID string) string {
	if itemID == "" {
		return "Unknown"
	}
	if strings.HasPrefix(itemID, "lib:") {
		return "Track"
	}
	if u, err := url.Parse(itemID); err == nil && u.Scheme != "" && u.Path != "" {
		base := path.Base(u.Path)
		if i := strings.LastIndex(base, "."); i > 0 {
			base = base[:i]
		}
		if decoded, err := url.PathUnescape(base); err == nil {
			return decoded
		}
		return base
	}
	return itemID
}

func (p *Popup) fetchArtwork(artURL string) {
	resp, err := http.Get(artURL)
	if err != nil {
		glib.IdleAdd(func() bool { p.setDefaultArtwork(); return false })
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		glib.IdleAdd(func() bool { p.setDefaultArtwork(); return false })
		return
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		glib.IdleAdd(func() bool { p.setDefaultArtwork(); return false })
		return
	}
	glib.IdleAdd(func() bool {
		loader, err := gdk.PixbufLoaderNew()
		if err != nil {
			p.setDefaultArtwork()
			return false
		}
		loader.Write(data)
		loader.Close()
		pixbuf, err := loader.GetPixbuf()
		if err != nil {
			p.setDefaultArtwork()
			return false
		}
		// Scale preserving aspect ratio to fit 56x56
		origW := pixbuf.GetWidth()
		origH := pixbuf.GetHeight()
		targetW, targetH := 56, 56
		if origW > 0 && origH > 0 {
			ratio := float64(origW) / float64(origH)
			if ratio > 1 {
				targetH = int(56 / ratio)
			} else {
				targetW = int(56 * ratio)
			}
		}
		scaled, err := pixbuf.ScaleSimple(targetW, targetH, gdk.INTERP_BILINEAR)
		if err != nil {
			p.artworkImg.SetFromPixbuf(pixbuf)
			return false
		}
		p.artworkImg.SetFromPixbuf(scaled)
		return false
	})
}

func notifyTrackChange(title, body, artURL string) {
	args := []string{"-a", "mu-applet", "-t", "3000"}
	if artURL != "" {
		args = append(args, "-i", artURL)
	} else {
		args = append(args, "-i", "audio-x-generic")
	}
	args = append(args, title, body)
	exec.Command("notify-send", args...).Run()
}

func (p *Popup) setDefaultArtwork() {
	p.artworkImg.SetFromIconName("audio-x-generic", gtk.ICON_SIZE_DIALOG)
}

func setButtonIcon(btn *gtk.Button, iconName string) {
	img, _ := gtk.ImageNewFromIconName(iconName, gtk.ICON_SIZE_SMALL_TOOLBAR)
	btn.SetImage(img)
}

func formatDuration(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
