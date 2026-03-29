//go:build gtk

package applet

import (
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/gtk"
)

// TrayIcon manages the system tray status icon.
type TrayIcon struct {
	icon           *gtk.StatusIcon
	popup          *Popup
	onQuit         func()
	onLeaseAcquire func()
	onLeaseRelease func()
	hasLease       bool
}

// NewTrayIcon creates a system tray icon.
func NewTrayIcon(onQuit func(), onLeaseAcquire func(), onLeaseRelease func()) (*TrayIcon, error) {
	icon, err := gtk.StatusIconNewFromIconName("multimedia-player")
	if err != nil {
		return nil, err
	}
	icon.SetTooltipText("mu-applet")
	icon.SetVisible(true)

	t := &TrayIcon{
		icon:           icon,
		onQuit:         onQuit,
		onLeaseAcquire: onLeaseAcquire,
		onLeaseRelease: onLeaseRelease,
		hasLease:       true,
	}

	icon.Connect("activate", t.onActivate)
	icon.Connect("popup-menu", t.onPopupMenu)

	return t, nil
}

// SetPopup associates a popup window with this tray icon.
func (t *TrayIcon) SetPopup(popup *Popup) {
	t.popup = popup
}

// SetHasLease updates the lease state for the context menu.
func (t *TrayIcon) SetHasLease(has bool) {
	t.hasLease = has
}

// SetPlaybackState updates the tray icon based on playback status.
func (t *TrayIcon) SetPlaybackState(status string) {
	switch status {
	case "playing":
		t.icon.SetFromIconName("media-playback-start")
		t.icon.SetTooltipText("mu-applet: playing")
	case "paused":
		t.icon.SetFromIconName("media-playback-pause")
		t.icon.SetTooltipText("mu-applet: paused")
	default:
		t.icon.SetFromIconName("multimedia-player")
		t.icon.SetTooltipText("mu-applet")
	}
}

func (t *TrayIcon) onActivate() {
	if t.popup == nil {
		return
	}
	if t.popup.IsVisible() {
		t.popup.Hide()
		return
	}
	t.popup.ShowCentered()
}

func (t *TrayIcon) onPopupMenu(_ *gtk.StatusIcon, button uint, activateTime uint32) {
	menu, _ := gtk.MenuNew()

	if t.hasLease {
		releaseItem, _ := gtk.MenuItemNewWithLabel("Release Control")
		releaseItem.Connect("activate", func() {
			if t.onLeaseRelease != nil {
				t.onLeaseRelease()
			}
		})
		menu.Append(releaseItem)
	} else {
		acquireItem, _ := gtk.MenuItemNewWithLabel("Take Control")
		acquireItem.Connect("activate", func() {
			if t.onLeaseAcquire != nil {
				t.onLeaseAcquire()
			}
		})
		menu.Append(acquireItem)
	}

	sepItem, _ := gtk.SeparatorMenuItemNew()
	menu.Append(sepItem)

	quitItem, _ := gtk.MenuItemNewWithLabel("Quit")
	quitItem.Connect("activate", func() {
		if t.onQuit != nil {
			t.onQuit()
		}
	})
	menu.Append(quitItem)

	menu.ShowAll()
	menu.PopupAtStatusIcon(t.icon, gdk.Button(button), activateTime)
}
