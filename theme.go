// theme.go — Custom Fyne theme implementations for GoVid.
//
// Provides two themes selectable from the Preferences window:
//   - darkTheme:  dark background with a cyan accent and tighter padding.
//   - lightTheme: light background with the same cyan accent.
//
// Both themes share the accentCyan colour constant to keep the palette
// consistent regardless of which theme is active.
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

// ── Log / status colour palette ───────────────────────────────────────────────
// Named colours used for log-line colouring and the status dot indicator.
// All inline color.RGBA literals for log messages and UI state reference one
// of these instead of repeating magic numbers across files.
var (
	// Log line colours
	colSystem        = color.RGBA{R: 0, G: 255, B: 255, A: 255}   // [SYSTEM] info lines
	colInfo          = color.RGBA{R: 0, G: 200, B: 200, A: 255}   // secondary info (batch, URL separator)
	colError         = color.RGBA{R: 255, G: 0, B: 0, A: 255}     // [ERROR] lines
	colWarning       = color.RGBA{R: 255, G: 165, B: 0, A: 255}   // [WARNING] / canceled
	colSuccess       = color.RGBA{R: 0, G: 200, B: 0, A: 255}     // success text
	colSuccessBorder = color.RGBA{R: 0, G: 255, B: 0, A: 255}     // success divider lines (────)
	colDebug         = color.RGBA{R: 180, G: 180, B: 180, A: 255} // [debug] / verbose output

	// Status dot colours
	colDotIdle       = color.RGBA{R: 100, G: 100, B: 115, A: 255} // idle / not running
	colDotSuccess    = color.RGBA{R: 0, G: 200, B: 80, A: 255}    // completed successfully
	colDotFailed     = color.RGBA{R: 220, G: 50, B: 50, A: 255}   // failed
	colDotCanceled   = color.RGBA{R: 255, G: 140, B: 0, A: 255}   // canceled by user
	colDotProcessing = color.RGBA{R: 180, G: 80, B: 255, A: 255}  // post-processing pulse base (alpha varies)
)

// ── Extended colour palette ───────────────────────────────────────────────────
var (
	colOutputLine    = color.RGBA{R: 220, G: 220, B: 220, A: 255} // raw tool output (e.g. yt-dlp update lines)
	colVerbose       = color.RGBA{R: 160, G: 160, B: 160, A: 255} // verbose / FFmpeg stderr stream output
	colCaution       = color.RGBA{R: 255, G: 200, B: 0, A: 255}   // soft-yellow caution (e.g. slow VP9 encoder)
	colErrorSoft     = color.RGBA{R: 255, G: 80, B: 80, A: 255}   // non-fatal error (e.g. file rename failure)
	colPPBorder      = color.RGBA{R: 0, G: 160, B: 0, A: 255}     // post-processing success divider (────)
	colAbortedBorder = color.RGBA{R: 255, G: 165, B: 255, A: 255} // ABORTED section divider (pink/magenta)

	// Processing-load indicator — unlit block and five increasing load levels.
	colLoadEmpty   = color.RGBA{R: 55, G: 55, B: 65, A: 255}
	colLoadPalette = []color.RGBA{
		{R: 0, G: 210, B: 90, A: 255},  // green       (lightest)
		{R: 140, G: 210, B: 0, A: 255}, // yellow-green
		{R: 230, G: 185, B: 0, A: 255}, // yellow
		{R: 230, G: 100, B: 0, A: 255}, // orange
		{R: 220, G: 45, B: 45, A: 255}, // red          (heaviest)
	}
)

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
