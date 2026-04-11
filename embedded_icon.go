package main

import _ "embed"
import "fyne.io/fyne/v2"

//go:embed appicon.png
var appIconBytes []byte

var resourceAppiconPng = &fyne.StaticResource{
	StaticName:    "appicon.png",
	StaticContent: appIconBytes,
}
