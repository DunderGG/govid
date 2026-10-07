// filename_template.go — The Filename Template setting.
//
// Responsibilities:
//   - outputTemplate: the -o template a download runs with: the user's
//     template (yt-dlp's own syntax, plus GoVid's {quality} placeholder for
//     the height label of capped downloads), then "_TRIM" for a trimmed
//     download and "_<download ID>" (which FinalizeFiles strips), then
//     ".%(ext)s".
//   - validateFilenameTemplate / filenameTemplateWarning: what a template
//     may not be (empty, or with a folder separator: FinalizeFiles looks for
//     files in the save folder only) and what it should have (%(title)s or
//     %(id)s, or every file gets the same name plus a number).
//   - previewFilename: the name a template gives a sample video, filling in
//     the common fields (title, id, uploader, upload_date, height, ext) the
//     way yt-dlp does, for the live preview in Preferences.
package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// defaultFilenameTemplate gives the names GoVid has always used.
const defaultFilenameTemplate = "GoVid_%(title)s" + qualityPlaceholder

// qualityPlaceholder stands for the height label (see heightLabel) of a
// download with a quality cap; it is empty for other downloads.
const qualityPlaceholder = "{quality}"

// validateFilenameTemplate reports why template cannot be used, or nil.
func validateFilenameTemplate(template string) error {
	switch {
	case strings.TrimSpace(template) == "":
		return errors.New("the filename template is empty")
	case strings.ContainsAny(template, `/\`):
		return errors.New("the filename template cannot hold / or \\: downloads are saved directly in the save folder")
	default:
		return nil
	}
}

// titleOrIDField matches a template field that tells videos apart.
var titleOrIDField = regexp.MustCompile(`%\((title|id)\b`)

// filenameTemplateWarning returns a warning for a usable template that will
// still name files badly, or "".
func filenameTemplateWarning(template string) string {
	if titleOrIDField.MatchString(template) {
		return ""
	}
	return "The template has neither %(title)s nor %(id)s, so every download gets the same name plus a number."
}

// outputTemplate returns the -o template for a download with the user's
// filename template (default when empty or invalid), the quality suffix
// (heightLabel for a capped download, else ""), and the download ID.
func outputTemplate(template, qualitySuffix, downloadID string, trimmed bool) string {
	if validateFilenameTemplate(template) != nil {
		template = defaultFilenameTemplate
	}
	name := strings.ReplaceAll(template, qualityPlaceholder, qualitySuffix)
	if trimmed {
		name += "_TRIM"
	}
	return name + "_" + downloadID + ".%(ext)s"
}

// previewSample is the video the preview names.
var previewSample = map[string]string{
	"title":       "Never Gonna Give You Up",
	"id":          "dQw4w9WgXcQ",
	"uploader":    "Rick Astley",
	"upload_date": "20091025",
	"height":      "1080",
	"ext":         "mp4",
}

// templateField matches one field of yt-dlp's output template, e.g.
// "%(title)s", "%(title).50s", "%(height)03d", "%(upload_date>%Y-%m-%d)s",
// "%(uploader|Unknown)s", or "%(height&_{}p|)s".
var templateField = regexp.MustCompile(`%\(([^)]*)\)([-#0 +]*\d*(?:\.\d+)?)([diouxXeEfFgGcrsaBlqDSUj])`)

// previewFilename returns the file name template gives previewSample when
// downloaded as format and quality, as yt-dlp would write it once GoVid
// has removed its download ID. unknown lists the fields the preview does
// not know; they are left as written, and yt-dlp fills them in (or writes
// "NA").
func previewFilename(template, format, quality string) (name string, unknown []string) {
	_, extension, height := formatSelection(format, quality)
	suffix := ""
	if height != "" {
		suffix = heightLabel
	}
	if validateFilenameTemplate(template) != nil {
		template = defaultFilenameTemplate
	}
	expanded := strings.ReplaceAll(template, qualityPlaceholder, suffix) + ".%(ext)s"
	sample := withValue(previewSample, "ext", extension)
	name = templateField.ReplaceAllStringFunc(expanded, func(field string) string {
		parts := templateField.FindStringSubmatch(field)
		value, known := renderField(parts[1], parts[2], parts[3], sample)
		if !known {
			unknown = append(unknown, parts[1])
			return field
		}
		return value
	})
	return name, unknown
}

// withValue returns a copy of values with key set to value.
func withValue(values map[string]string, key, value string) map[string]string {
	copied := make(map[string]string, len(values)+1)
	for k, v := range values {
		copied[k] = v
	}
	copied[key] = value
	return copied
}

// renderField fills in one template field from sample. spec and verb are
// the printf flags, width, and precision, and conversion. known is false
// for a field not in sample.
func renderField(name, spec, verb string, sample map[string]string) (value string, known bool) {
	// "field|default": the default when the field is empty or missing.
	fieldPart, fallback, hasFallback := strings.Cut(name, "|")
	// "field&replacement": the replacement, with {} for the value, when
	// the field has one.
	fieldPart, replacement, hasReplacement := strings.Cut(fieldPart, "&")
	// "field>format": a date formatted with strftime directives.
	fieldName, dateFormat, hasDate := strings.Cut(fieldPart, ">")

	raw, found := sample[fieldName]
	if !found && !hasFallback {
		return "", false
	}
	if !found || raw == "" {
		return fallback, true
	}
	if hasDate {
		if date, err := time.Parse("20060102", raw); err == nil {
			raw = strftime(date, dateFormat)
		}
	}
	formatted := formatValue(raw, spec, verb)
	if hasReplacement {
		return strings.ReplaceAll(replacement, "{}", formatted), true
	}
	return formatted, true
}

// formatValue applies a printf conversion to a field's value: a precision
// cuts a string, and an integer conversion pads a number.
func formatValue(value, spec, verb string) string {
	switch verb {
	case "d", "i":
		if n, err := strconv.Atoi(value); err == nil {
			return fmt.Sprintf("%"+spec+"d", n)
		}
	case "s":
		if _, precision, ok := strings.Cut(spec, "."); ok {
			if limit, err := strconv.Atoi(precision); err == nil && utf8.RuneCountInString(value) > limit {
				value = string([]rune(value)[:limit])
			}
		}
	}
	return value
}

// strftime formats date with the common strftime directives yt-dlp
// accepts in a "field>format" template field.
func strftime(date time.Time, format string) string {
	replacer := strings.NewReplacer(
		"%Y", date.Format("2006"), "%y", date.Format("06"), "%m", date.Format("01"),
		"%d", date.Format("02"), "%B", date.Format("January"), "%b", date.Format("Jan"),
		"%H", "00", "%M", "00", "%S", "00", "%%", "%",
	)
	return replacer.Replace(format)
}

// buildFilenameTemplateRow lays out the Filename Template entry with its
// Reset button and, below it, a live preview: the name the template gives
// a sample video with the current Format and Max Quality, plus why the
// template is refused, a warning, or the fields only yt-dlp fills in.
func (manager *UIManager) buildFilenameTemplateRow() fyne.CanvasObject {
	entry := manager.ui.prefs.filenameTemplate
	preview := widget.NewLabel("")
	preview.Wrapping = fyne.TextWrapWord
	preview.TextStyle = fyne.TextStyle{Italic: true}
	refresh := func(template string) {
		preview.SetText(templatePreviewText(template, manager.ui.download.format.Selected, manager.ui.download.quality.Selected))
	}
	entry.OnChanged = refresh
	reset := widget.NewButton("Reset", func() { entry.SetText(defaultFilenameTemplate) })
	refresh(entry.Text)
	return container.NewVBox(container.NewBorder(nil, nil, nil, reset, entry), preview)
}

// templatePreviewText is the preview shown under the Filename Template.
func templatePreviewText(template, format, quality string) string {
	if err := validateFilenameTemplate(template); err != nil {
		return "Not saved: " + err.Error() + "."
	}
	name, unknown := previewFilename(template, format, quality)
	lines := []string{"Example: " + name}
	if warning := filenameTemplateWarning(template); warning != "" {
		lines = append(lines, "⚠ "+warning)
	}
	if len(unknown) > 0 {
		lines = append(lines, fmt.Sprintf("Shown as written: %s. yt-dlp fills these in, or writes NA when a site does not have them.", strings.Join(unknown, ", ")))
	}
	return strings.Join(lines, "\n")
}
