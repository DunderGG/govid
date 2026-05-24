// icons.go — SVG icon resources and the themedIcon helper.
//
// Each icon is a 24×24 Material-style filled SVG that exists in two colour
// variants: a soft-white fill for the dark theme and a cyan fill for the
// light theme. The themedIcon() helper returns the correct variant based
// on the currently active theme variant.
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
)

type iconDarkLight struct {
	dark  *fyne.StaticResource
	light *fyne.StaticResource
}

// IconName identifies a named icon in the GoVid icon set.
type IconName int

const (
	IconDownload IconName = iota
	IconFolderOpen
	IconFolder
	IconCancel
)

// Fill colours for the two theme variants.
const (
	svgFillDark  = "#E6E6F0"
	svgFillLight = "#1C9BBE"
)

// SVG path data for each icon (24×24 Material-style).
const (
	svgPathDownload   = `M5 20h14v-2H5v2zm7-18v11.17l-3.59-3.58L7 11l5 5 5-5-1.41-1.41L13 13.17V2h-1z`
	svgPathFolderOpen = `M20 6h-8l-2-2H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z`
	svgPathFolder     = `M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z`
	svgPathCancel     = `M12 2C6.47 2 2 6.47 2 12s4.47 10 10 10 10-4.47 10-10S17.53 2 12 2zm5 13.59L15.59 17 12 13.41 8.41 17 7 15.59 10.59 12 7 8.41 8.41 7 12 10.59 15.59 7 17 8.41 13.41 12 17 15.59z`
)

var icons = map[IconName]iconDarkLight{
	IconDownload: {
		dark:  svgWithColor("download.svg", svgPathDownload, svgFillDark),
		light: svgWithColor("download_light.svg", svgPathDownload, svgFillLight),
	},
	IconFolderOpen: {
		dark:  svgWithColor("folder_open.svg", svgPathFolderOpen, svgFillDark),
		light: svgWithColor("folder_open_light.svg", svgPathFolderOpen, svgFillLight),
	},
	IconFolder: {
		dark:  svgWithColor("folder.svg", svgPathFolder, svgFillDark),
		light: svgWithColor("folder_light.svg", svgPathFolder, svgFillLight),
	},
	IconCancel: {
		dark:  svgWithColor("cancel.svg", svgPathCancel, svgFillDark),
		light: svgWithColor("cancel_light.svg", svgPathCancel, svgFillLight),
	},
}

// svgWithColor wraps a 24×24 SVG path in the standard icon template using the
// given fill colour, returning a ready-to-use Fyne StaticResource.
func svgWithColor(name, pathData, fill string) *fyne.StaticResource {
	svg := fmt.Sprintf(
		"<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\">\n"+
			"  <path fill=\"%s\" d=\"%s\"/>\n"+
			"</svg>",
		fill, pathData,
	)
	return fyne.NewStaticResource(name, []byte(svg))
}

// themedIcon returns the correct dark or light icon variant for the active theme.
func themedIcon(name IconName) fyne.Resource {
	icon := icons[name]
	if fyne.CurrentApp().Preferences().StringWithFallback("themeMode", "Dark") == "Light" {
		return icon.light
	}
	return icon.dark
}
