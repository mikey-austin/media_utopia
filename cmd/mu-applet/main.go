//go:build gtk

package main

import (
	"fmt"
	"os"

	"github.com/gotk3/gotk3/gtk"
)

func main() {
	gtk.Init(nil)
	fmt.Fprintln(os.Stderr, "mu-applet: gtk init ok")
}
