package main

import (
	"fyne.io/fyne/v2"
)

// IconName identifies a named icon in the GoVid icon set.
type IconName int

const (
	IconDownload IconName = iota
	IconFolderOpen
	IconFolder
	IconCancel
)
// Each icon is a 24×24 Material-style filled SVG using a single solid colour for the fill.
// Each icon exists in two colour variants stored in a lookup table:
//   - dark variant: soft-white fill (#E6E6F0) — readable on the dark theme background.
//   - light variant: accent-cyan fill (#1C9BBE) — matches the card border on the light theme.

type iconDarkLight struct {
	dark  *fyne.StaticResource
	light *fyne.StaticResource
}

var icons = map[IconName]iconDarkLight{
	IconDownload: {
		dark: fyne.NewStaticResource("download.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M5 20h14v-2H5v2zm7-18v11.17l-3.59-3.58L7 11l5 5 5-5-1.41-1.41L13 13.17V2h-1z"/>
</svg>`)),
		light: fyne.NewStaticResource("download_light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#1C9BBE" d="M5 20h14v-2H5v2zm7-18v11.17l-3.59-3.58L7 11l5 5 5-5-1.41-1.41L13 13.17V2h-1z"/>
</svg>`)),
	},
	IconFolderOpen: {
		dark: fyne.NewStaticResource("folder_open.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M20 6h-8l-2-2H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z"/>
</svg>`)),
		light: fyne.NewStaticResource("folder_open_light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#1C9BBE" d="M20 6h-8l-2-2H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z"/>
</svg>`)),
	},
	IconFolder: {
		dark: fyne.NewStaticResource("folder.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/>
</svg>`)),
		light: fyne.NewStaticResource("folder_light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#1C9BBE" d="M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/>
</svg>`)),
	},
	IconCancel: {
		dark: fyne.NewStaticResource("cancel.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M12 2C6.47 2 2 6.47 2 12s4.47 10 10 10 10-4.47 10-10S17.53 2 12 2zm5 13.59L15.59 17 12 13.41 8.41 17 7 15.59 10.59 12 7 8.41 8.41 7 12 10.59 15.59 7 17 8.41 13.41 12 17 15.59z"/>
</svg>`)),
		light: fyne.NewStaticResource("cancel_light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#1C9BBE" d="M12 2C6.47 2 2 6.47 2 12s4.47 10 10 10 10-4.47 10-10S17.53 2 12 2zm5 13.59L15.59 17 12 13.41 8.41 17 7 15.59 10.59 12 7 8.41 8.41 7 12 10.59 15.59 7 17 8.41 13.41 12 17 15.59z"/>
</svg>`)),
	},
}

// themedIcon returns the correct dark or light icon variant for the active theme.
func themedIcon(name IconName) fyne.Resource {
	icon := icons[name]
	if fyne.CurrentApp().Preferences().StringWithFallback("themeMode", "Dark") == "Light" {
		return icon.light
	}
	return icon.dark
}
