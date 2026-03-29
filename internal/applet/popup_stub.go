//go:build gtk

package applet

// Popup is a stub for the popup window, to be implemented in Task 6.
// This file exists only so that tray.go compiles before the full
// popup implementation is added.
type Popup struct{}

func (p *Popup) IsVisible() bool                { return false }
func (p *Popup) Hide()                          {}
func (p *Popup) ShowAt(x, y, width, height int) {}
func (p *Popup) ShowCentered()                  {}
