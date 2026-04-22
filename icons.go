package main

import "fyne.io/fyne/v2"

// Each icon is a 24×24 Material-style filled SVG using the soft-white foreground
// colour (#E6E6F0) so they render cleanly on the dark theme background.

// iconDownload is a filled downward-arrow into a tray, representing "Download Now".
var iconDownload = fyne.NewStaticResource("download.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M5 20h14v-2H5v2zm7-18v11.17l-3.59-3.58L7 11l5 5 5-5-1.41-1.41L13 13.17V2h-1z"/>
</svg>`))

// iconFolderOpen is a filled open-folder with an upward arrow, used for the browse button.
var iconFolderOpen = fyne.NewStaticResource("folder_open.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M20 6h-8l-2-2H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z"/>
</svg>`))

// iconFolder is a plain filled folder, used for the "Open Folder" button.
var iconFolder = fyne.NewStaticResource("folder.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/>
</svg>`))

// iconCancel is a filled circle with an X, used for the Cancel button.
var iconCancel = fyne.NewStaticResource("cancel.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path fill="#E6E6F0" d="M12 2C6.47 2 2 6.47 2 12s4.47 10 10 10 10-4.47 10-10S17.53 2 12 2zm5 13.59L15.59 17 12 13.41 8.41 17 7 15.59 10.59 12 7 8.41 8.41 7 12 10.59 15.59 7 17 8.41 13.41 12 17 15.59z"/>
</svg>`))
