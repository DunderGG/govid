package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// darkTheme is a custom Fyne theme that gives GoVid a dark look with a
// vivid cyan accent colour, tighter padding, and a slightly larger base font.
type darkTheme struct{}

var _ fyne.Theme = (*darkTheme)(nil)

// Accent is the primary highlight colour used on focused widgets, buttons, etc.
// Matched to the steel-cyan of the GoVid app icon background.
var accentCyan = color.RGBA{R: 28, G: 155, B: 190, A: 255} // #1C9BBE

func (t *darkTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	// We always render in dark mode regardless of the OS system variant.
	// The variant argument is intentionally ignored here.
	switch name {
	case theme.ColorNameBackground:
		return color.RGBA{R: 18, G: 18, B: 24, A: 255} // near-black
	case theme.ColorNameButton:
		return color.RGBA{R: 30, G: 30, B: 40, A: 255}
	case theme.ColorNameDisabledButton:
		return color.RGBA{R: 30, G: 30, B: 40, A: 128}
	case theme.ColorNameDisabled:
		return color.RGBA{R: 100, G: 100, B: 115, A: 255}
	case theme.ColorNameForeground:
		return color.RGBA{R: 230, G: 230, B: 240, A: 255} // soft white
	case theme.ColorNameInputBackground:
		return color.RGBA{R: 26, G: 26, B: 36, A: 255}
	case theme.ColorNameInputBorder:
		return color.RGBA{R: 55, G: 55, B: 70, A: 255}
	case theme.ColorNameMenuBackground:
		return color.RGBA{R: 24, G: 24, B: 32, A: 255}
	case theme.ColorNameOverlayBackground:
		return color.RGBA{R: 24, G: 24, B: 32, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.RGBA{R: 100, G: 100, B: 115, A: 255}
	case theme.ColorNamePressed:
		return color.RGBA{R: 18, G: 120, B: 148, A: 255} // slightly darker than accent
	case theme.ColorNamePrimary:
		return accentCyan
	case theme.ColorNameFocus:
		return accentCyan
	case theme.ColorNameHover:
		return color.RGBA{R: 40, G: 40, B: 55, A: 255}
	case theme.ColorNameScrollBar:
		return color.RGBA{R: 60, G: 60, B: 80, A: 200}
	case theme.ColorNameSeparator:
		return color.RGBA{R: 45, G: 45, B: 60, A: 255}
	case theme.ColorNameShadow:
		return color.RGBA{R: 0, G: 0, B: 0, A: 120}
	case theme.ColorNameHeaderBackground:
		return color.RGBA{R: 22, G: 22, B: 30, A: 255}
	case theme.ColorNameSuccess:
		return color.RGBA{R: 0, G: 200, B: 120, A: 255}
	case theme.ColorNameWarning:
		return color.RGBA{R: 255, G: 165, B: 0, A: 255}
	case theme.ColorNameError:
		return color.RGBA{R: 220, G: 60, B: 60, A: 255}
	case theme.ColorNameSelection:
		return color.RGBA{R: 28, G: 155, B: 190, A: 60}
	}
	// Fall through to the built-in default dark theme for anything not explicitly overridden.
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}

func (t *darkTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *darkTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *darkTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return 13 // slightly larger than default (12)
	}
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameLineSpacing:
		return 4
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 4
	}
	return theme.DefaultTheme().Size(name)
}

// lightTheme wraps Fyne's DefaultTheme and forces the Light variant
// regardless of what the OS system theme is. This ensures the Light option
// in Preferences always produces a genuine light UI.
type lightTheme struct{}

var _ fyne.Theme = (*lightTheme)(nil)

func (t *lightTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(name, theme.VariantLight)
}

func (t *lightTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *lightTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *lightTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}
