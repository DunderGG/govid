// formats.go — The formats a site offers for a video, and choosing them.
//
// Responsibilities:
//   - FormatInfo: one format of a video's info JSON (yt-dlp's "formats"
//     list): resolution, frame rate, dynamic range, codecs, bitrate,
//     container, and size, and whether it is video only, audio only, or
//     both. Storyboards (thumbnail sheets) are left out.
//   - formatRows: the Format Browser's table, in yt-dlp's own order (that
//     of "yt-dlp -F"), filtered by kind.
//   - selectedFormats / describeDownload: which formats a download will
//     fetch (the probe's requested formats, or the user's pick), for the
//     "Will download: 1080p AV1 + Opus → MP4 (~45 MiB)" line.
//   - formatArgs: the -f selector (the user's pick, or the Format and Max
//     Quality settings) and the -S sort that the Preferred Video Codec
//     setting adds.
//
// The window itself is in formats_window.go.
package main

import (
	"fmt"
	"strings"
)

// FormatInfo is one format yt-dlp lists for a video.
type FormatInfo struct {
	ID           string  `json:"format_id"`
	Ext          string  `json:"ext"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	FPS          float64 `json:"fps"`
	VCodec       string  `json:"vcodec"`
	ACodec       string  `json:"acodec"`
	TBR          float64 `json:"tbr"` // total bitrate, kbit/s
	ABR          float64 `json:"abr"`
	VBR          float64 `json:"vbr"`
	DynamicRange string  `json:"dynamic_range"` // "SDR", "HDR10", "HLG", …
	Note         string  `json:"format_note"`
	Protocol     string  `json:"protocol"`
	formatSize
}

// formatKind is what a format holds.
type formatKind int

const (
	kindOther    formatKind = iota // storyboards and other images
	kindVideo                      // video only
	kindAudio                      // audio only
	kindCombined                   // video and audio
)

// hasCodec reports whether a vcodec or acodec value names a codec, rather
// than "none" (or nothing).
func hasCodec(codec string) bool {
	return codec != "" && codec != "none"
}

// kind returns what the format holds.
func (format FormatInfo) kind() formatKind {
	// A codec yt-dlp does not know is left out, not "none": such a stream
	// is there, as "yt-dlp -F" lists it ("unknown").
	video := format.VCodec != "none" && format.VCodec != "images"
	audio := format.ACodec != "none"
	switch {
	case format.Note == "storyboard" || format.VCodec == "images" || format.Ext == "mhtml":
		return kindOther
	case video && audio:
		return kindCombined
	case video:
		return kindVideo
	case audio:
		return kindAudio
	default:
		return kindOther
	}
}

// videoCodecName names a vcodec value as people know it, e.g. "H.264" for
// "avc1.640028"; other values are returned as they are.
func videoCodecName(codec string) string {
	lower := strings.ToLower(codec)
	switch {
	case !hasCodec(codec):
		return ""
	case strings.HasPrefix(lower, "avc"), strings.HasPrefix(lower, "h264"):
		return "H.264"
	case strings.HasPrefix(lower, "hev"), strings.HasPrefix(lower, "hvc"), strings.HasPrefix(lower, "h265"):
		return "H.265"
	case strings.HasPrefix(lower, "vp09"), strings.HasPrefix(lower, "vp9"):
		return "VP9"
	case strings.HasPrefix(lower, "av01"), lower == "av1":
		return "AV1"
	case strings.HasPrefix(lower, "vp8"):
		return "VP8"
	default:
		return codec
	}
}

// audioCodecName names an acodec value as people know it, e.g. "AAC" for
// "mp4a.40.2".
func audioCodecName(codec string) string {
	lower := strings.ToLower(codec)
	switch {
	case !hasCodec(codec):
		return ""
	case strings.HasPrefix(lower, "mp4a"), lower == "aac":
		return "AAC"
	case lower == "opus":
		return "Opus"
	case lower == "vorbis":
		return "Vorbis"
	case strings.HasPrefix(lower, "ac-3"), lower == "ac3":
		return "AC-3"
	case strings.HasPrefix(lower, "ec-3"), lower == "eac3":
		return "E-AC-3"
	case lower == "mp3":
		return "MP3"
	default:
		return codec
	}
}

// resolutionText is the format's picture size, "1280x720", or "audio only".
func (format FormatInfo) resolutionText() string {
	switch {
	case format.kind() == kindAudio:
		return "audio only"
	case format.Width > 0 && format.Height > 0:
		return fmt.Sprintf("%dx%d", format.Width, format.Height)
	case format.Height > 0:
		return fmt.Sprintf("%dp", format.Height)
	default:
		return ""
	}
}

// approxBytes returns the format's size: exact, or the site's estimate, or
// one worked out from its bitrate and the video's duration, as yt-dlp -F
// shows it. approx is true unless the size is exact.
func (format FormatInfo) approxBytes(duration float64) (size float64, approx, known bool) {
	switch {
	case format.FileSize > 0:
		return format.FileSize, false, true
	case format.FileSizeApprox > 0:
		return format.FileSizeApprox, true, true
	case format.TBR > 0 && duration > 0:
		return format.TBR * 1000 / 8 * duration, true, true
	default:
		return 0, false, false
	}
}

// sizeText is the format's size for the table: "16.9 MiB", "~ 2.0 MiB", or "".
func (format FormatInfo) sizeText(duration float64) string {
	size, approx, known := format.approxBytes(duration)
	if !known {
		return ""
	}
	if approx {
		return "~ " + formatBytes(int64(size))
	}
	return formatBytes(int64(size))
}

// bitrateText is the format's total bitrate, e.g. "664k".
func (format FormatInfo) bitrateText() string {
	rate := format.TBR
	if rate <= 0 {
		rate = format.VBR + format.ABR
	}
	if rate <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0fk", rate)
}

// hdrText says whether the format is HDR: its dynamic range, or "".
func (format FormatInfo) hdrText() string {
	if format.DynamicRange == "" || format.DynamicRange == "SDR" {
		return ""
	}
	return format.DynamicRange
}

// fpsText is the frame rate, e.g. "25", or "".
func (format FormatInfo) fpsText() string {
	if format.FPS <= 0 || format.kind() == kindAudio {
		return ""
	}
	return fmt.Sprintf("%g", format.FPS)
}

// formatFilter narrows the Format Browser's table.
type formatFilter int

const (
	filterAll formatFilter = iota
	filterVideo
	filterAudio
	filterCombined
)

// formatFilterOptions are the filter's labels, in display order.
var formatFilterOptions = []string{"All", "Video only", "Audio only", "Video + audio"}

// matches reports whether format passes the filter.
func (filter formatFilter) matches(format FormatInfo) bool {
	switch filter {
	case filterVideo:
		return format.kind() == kindVideo
	case filterAudio:
		return format.kind() == kindAudio
	case filterCombined:
		return format.kind() == kindCombined
	default:
		return format.kind() != kindOther
	}
}

// formatColumns are the Format Browser's columns.
var formatColumns = []string{"ID", "Resolution", "FPS", "HDR", "Video", "Audio", "Bitrate", "Container", "Size"}

// formatRow is one row of the Format Browser's table.
type formatRow struct {
	format FormatInfo
	cells  []string // in formatColumns order
}

// formatRows returns the table rows of info's formats that pass filter, in
// the order yt-dlp lists them (worst first, as "yt-dlp -F" does), without
// storyboards.
func formatRows(info MediaInfo, filter formatFilter) []formatRow {
	var rows []formatRow
	for _, format := range info.Formats {
		if !filter.matches(format) {
			continue
		}
		rows = append(rows, formatRow{format: format, cells: []string{
			format.ID, format.resolutionText(), format.fpsText(), format.hdrText(),
			videoCodecName(format.VCodec), audioCodecName(format.ACodec),
			format.bitrateText(), format.Ext, format.sizeText(info.Duration),
		}})
	}
	return rows
}

// findFormat returns info's format with id.
func (info MediaInfo) findFormat(id string) (FormatInfo, bool) {
	for _, format := range info.Formats {
		if format.ID == id {
			return format, true
		}
	}
	return FormatInfo{}, false
}

// selectedFormats returns the formats a download with info fetches: those
// a pick ("247+251") names, or else the ones the probe's selector chose.
// ok is false when they are not known.
func selectedFormats(info MediaInfo, pick string) (formats []FormatInfo, ok bool) {
	ids := strings.Split(pick, "+")
	if pick == "" {
		if len(info.RequestedFormats) > 0 {
			return info.RequestedFormats, true
		}
		ids = []string{info.FormatID}
	}
	for _, id := range ids {
		format, found := info.findFormat(id)
		if !found {
			return nil, false
		}
		formats = append(formats, format)
	}
	return formats, len(formats) > 0
}

// describeDownload describes what a download fetches and makes, e.g.
// "1080p AV1 + Opus → MP4 (~45.2 MiB)". It returns "" when the formats are
// not known.
func describeDownload(info MediaInfo, pick, extension string) string {
	formats, ok := selectedFormats(info, pick)
	if !ok {
		return ""
	}
	var parts []string
	var total float64
	sizeKnown := true
	for _, format := range formats {
		if video := videoCodecName(format.VCodec); video != "" {
			part := video
			if format.Height > 0 {
				part = fmt.Sprintf("%dp %s", format.Height, video)
			}
			if hdr := format.hdrText(); hdr != "" {
				part += " " + hdr
			}
			parts = append(parts, part)
		}
		if audio := audioCodecName(format.ACodec); audio != "" {
			parts = append(parts, audio)
		}
		size, _, known := format.approxBytes(info.Duration)
		total += size
		sizeKnown = sizeKnown && known
	}
	if len(parts) == 0 {
		return ""
	}
	text := strings.Join(parts, " + ") + " → " + strings.ToUpper(extension)
	if sizeKnown && total > 0 {
		text += fmt.Sprintf(" (~%s)", formatBytes(int64(total)))
	}
	return text
}

// pickFormats returns the -f value for a chosen video and audio format: a
// combined format alone, or "video+audio". Either may be empty.
func pickFormats(video, audio FormatInfo) string {
	switch {
	case video.ID == "":
		return audio.ID
	case audio.ID == "" || video.kind() == kindCombined:
		return video.ID
	default:
		return video.ID + "+" + audio.ID
	}
}

// Preferred video codecs (Preferences "Preferred Video Codec").
const (
	codecAny  = "Any"
	codecH264 = "H.264"
	codecVP9  = "VP9"
	codecAV1  = "AV1"
)

// preferredCodecOptions lists the preferred video codecs in display order.
var preferredCodecOptions = []string{codecAny, codecH264, codecVP9, codecAV1}

// codecSort returns the yt-dlp -S value that prefers codec, or "" for Any.
// A codec named in -S ranks before resolution, so H.264 means "the best
// H.264", even when a sharper VP9 or AV1 version exists.
func codecSort(codec string) string {
	switch codec {
	case codecH264:
		return "vcodec:avc"
	case codecVP9:
		return "vcodec:vp9"
	case codecAV1:
		return "vcodec:av01"
	default:
		return ""
	}
}

// formatArgs returns req's format options for yt-dlp: -f with the user's
// pick (req.FormatPick) or the selector of its Format and Max Quality, and,
// without a pick, -S for the preferred codec. The probe and the download
// share them, so the probe reports what the download fetches.
func formatArgs(req DownloadRequest) []string {
	if req.FormatPick != "" {
		return []string{"-f", req.FormatPick}
	}
	formatFlag, _, _ := formatSelection(req.Format, req.Quality)
	args := []string{"-f", formatFlag}
	if sort := codecSort(req.PreferredCodec); sort != "" && !isAudioOnlyExt(formatExtension(req.Format)) {
		args = append(args, "-S", sort)
	}
	return args
}

// formatExtension returns the output extension of a Format choice.
func formatExtension(format string) string {
	_, extension, _ := formatSelection(format, qualityBest)
	return extension
}

// selectedIDs returns the IDs of the formats a download with info fetches,
// as yt-dlp names them ("401+251"), or "" when they are not known.
func selectedIDs(info MediaInfo, pick string) string {
	formats, ok := selectedFormats(info, pick)
	if !ok {
		return ""
	}
	ids := make([]string, len(formats))
	for i, format := range formats {
		ids[i] = format.ID
	}
	return strings.Join(ids, "+")
}
