// subtitles.go — Telling the user which subtitles a video has.
//
// Responsibilities:
//   - matchSubLangs: which of a video's subtitle languages a --sub-langs list
//     selects, following yt-dlp's own rules.
//   - reportSubtitles: before a download that asks for subtitles, logs the
//     languages the probe found and warns when none of them was asked for.
//
// The yt-dlp flags that fetch, convert, and embed subtitles are built by
// subtitleArgs in download_engine.go.
package main

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// maxListedLanguages is how many language codes a log line names before it
// only gives the count. YouTube offers automatic captions in over 150.
const maxListedLanguages = 12

// matchSubLangs returns the languages in available that a yt-dlp --sub-langs
// list selects, in the order yt-dlp would download them. Each comma-separated
// entry is a regular expression that must match a whole language code; "all"
// selects every language, and an entry starting with "-" removes the
// languages it matches from those selected so far. An entry that is not a
// valid expression selects nothing.
func matchSubLangs(spec string, available []string) []string {
	var selected []string
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		discard := strings.HasPrefix(entry, "-")
		entry = strings.TrimPrefix(entry, "-")
		if entry == "" {
			continue
		}
		if entry == "all" {
			if discard {
				selected = nil
				continue
			}
			for _, lang := range available {
				if !slices.Contains(selected, lang) {
					selected = append(selected, lang)
				}
			}
			continue
		}
		pattern, err := regexp.Compile("^(?:" + entry + ")$")
		if err != nil {
			continue
		}
		for _, lang := range available {
			if !pattern.MatchString(lang) {
				continue
			}
			if discard {
				selected = slices.DeleteFunc(selected, func(chosen string) bool { return chosen == lang })
			} else if !slices.Contains(selected, lang) {
				selected = append(selected, lang)
			}
		}
	}
	return selected
}

// subtitleLanguages returns the sorted language codes of a probe's
// subtitles, and of its automatic captions.
func subtitleLanguages(info MediaInfo) (manual, automatic []string) {
	return slices.Sorted(maps.Keys(info.Subtitles)), slices.Sorted(maps.Keys(info.AutomaticCaptions))
}

// describeLanguages lists language codes for the log, e.g. "en, de, fr", or
// "157 languages" when there are too many to name.
func describeLanguages(langs []string) string {
	if len(langs) > maxListedLanguages {
		return fmt.Sprintf("%d languages", len(langs))
	}
	return strings.Join(langs, ", ")
}

// reportSubtitles logs which subtitle languages the probe found for item,
// when req asks for subtitles, and which of them will be downloaded. It
// warns when none matches req's languages, in which case the video
// downloads without subtitles. Nothing is said for audio formats, or when
// the video was not probed.
func (app *DownloaderApp) reportSubtitles(item queueItem, req DownloadRequest) {
	_, extension, _ := formatSelection(req.Format, req.Quality)
	if item.info == nil || req.Subtitles == "" || req.Subtitles == subtitlesOff || isAudioOnlyExt(extension) {
		return
	}
	manual, automatic := subtitleLanguages(*item.info)
	available := manual
	switch {
	case len(manual) == 0 && len(automatic) == 0:
		app.appendOutput("[SYSTEM] This video has no subtitles.", colWarning)
		return
	case req.AutoSubtitles:
		available = slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(manual), automatic...))))
		app.appendOutput(fmt.Sprintf("[SYSTEM] Subtitles: %s; auto-generated: %s.", orNone(describeLanguages(manual)), orNone(describeLanguages(automatic))), colSystem)
	default:
		app.appendOutput(fmt.Sprintf("[SYSTEM] Subtitles: %s (auto-generated captions are off).", orNone(describeLanguages(manual))), colSystem)
	}

	langs := strings.TrimSpace(req.SubtitleLangs)
	if langs == "" {
		langs = defaultSubtitleLangs
	}
	chosen := matchSubLangs(langs, available)
	if len(chosen) == 0 {
		app.appendOutput(fmt.Sprintf("[SYSTEM] No subtitles match %q for this video; it will download without them.", langs), colWarning)
		return
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] Downloading subtitles: %s.", describeLanguages(chosen)), colSystem)
}

// orNone returns text, or "none" when it is empty.
func orNone(text string) string {
	if text == "" {
		return "none"
	}
	return text
}
