//go:build gtk

package applet

import (
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/gtk"
)

// TrayIcon manages the system tray status icon.
type TrayIcon struct {
	icon   *gtk.StatusIcon
	popup  *Popup
	onQuit func()
}

// NewTrayIcon creates a system tray icon. onQuit is called when the user selects Quit.
func NewTrayIcon(onQuit func()) (*TrayIcon, error) {
	icon, err := gtk.StatusIconNewFromIconName("multimedia-player")
	if err != nil {
		return nil, err
	}
	icon.SetTooltipText("mu-applet")
	icon.SetVisible(true)

	t := &TrayIcon{icon: icon, onQuit: onQuit}

	icon.Connect("activate", t.onActivate)
	icon.Connect("popup-menu", t.onPopupMenu)

	return t, nil
}

// SetPopup associates a popup window with this tray icon.
func (t *TrayIcon) SetPopup(popup *Popup) {
	t.popup = popup
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
	// StatusIcon.GetGeometry is not implemented in gotk3, so fall back
	// to centering the popup on screen.
	t.popup.ShowCentered()
}

func (t *TrayIcon) onPopupMenu(_ *gtk.StatusIcon, button uint, activateTime uint32) {
	menu, _ := gtk.MenuNew()
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
