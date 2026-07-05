// embedded_icon.go — Embeds the application icon into the binary.
//
// appicon.png is compiled directly into the executable at build time via
// go embed so that GoVid does not depend on the file being present at
// runtime. The resulting resourceAppiconPng is used as the window icon.
package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed appicon.png
var appIconBytes []byte

var resourceAppiconPng = &fyne.StaticResource{
	StaticName:    "appicon.png",
	StaticContent: appIconBytes,
}
