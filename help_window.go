// help_window.go — The Configuration Help window.
//
// Responsibilities:
//   - helpItems: the guide's text, one Markdown topic per setting in the
//     main window, Preferences, and Post-Processing.
//   - UIManager.showConfigHelp: the scrollable window that shows them.
//   - codeList: renders a list of options as Markdown code spans.
package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// helpItem is one topic of the Configuration Help window: a heading and its
// text, in Markdown.
type helpItem struct {
	label string
	desc  string
}

// helpItems are the Configuration Help window's topics, in the order shown.
var helpItems = []helpItem{
	{"Video URL", "Paste any URL supported by yt-dlp, such as a **YouTube**, **Vimeo**, or **Twitter/X** link.\n\nOther ways to add URLs (each switches on **Batch Mode** when the field then holds more than one, and skips URLs already in it):\n" +
		"  * **Load from file…** – reads a `.txt` file with one URL per line. Blank lines and lines starting with `#` are skipped, as are lines that are not links; the log says how many and which\n" +
		"  * **Paste button** (next to the field) – adds the links on the clipboard, one per line. It refuses the clipboard if anything in it is not a link\n" +
		"  * **Drop onto the window** – a `.txt` list, or an internet shortcut (`.url`). A link dragged straight from a browser is not received on Windows; drag it to the desktop first, then drop the shortcut that makes\n\n" +
		"In Batch Mode, lines starting with `#` are comments and are not downloaded."},
	{"Playlists", "Before downloading, GoVid checks each URL. When one is a playlist, it shows the playlist's title, number of videos, and total length, and asks which videos to download:\n  * Leave the range blank and click **Download** to get them all\n  * Enter a range such as `1-10`, `5-`, or `3,5,8` to get only those\n  * For a link to one video inside a playlist (`watch?v=…&list=…`), **Only this video** is the default\n\nEach chosen video becomes its own item in the queue, with its own progress, Cancel, and history entry."},
	{"Queue", "When a session has more than one video (Batch Mode, or videos picked from a playlist), the **Queue** panel above Terminal Output lists them with their status: Waiting, Checking, Downloading with its percentage, Paused, Post-processing, Done, Failed, or Skipped. Its title counts progress, e.g. \"7 of 20 done, 1 failed\". Click the title to fold the panel away.\n\nWhile the queue runs:\n" +
		"  * **Waiting** videos can be moved up or down (the order is the download order) or removed\n" +
		"  * The video **downloading** can be paused or skipped (the same as **Cancel**); the queue moves on\n" +
		"  * A **paused** video can be resumed (it goes first among the waiting ones and continues where it stopped) or removed, which deletes what it had downloaded\n" +
		"  * A video that **failed** or was **skipped** can be retried; it goes back to the end of the queue\n\n" +
		"Videos the queue did not get to, because it was stopped, are marked Skipped."},
	{"Simultaneous Downloads", "Found in **Tools → Preferences**. How many videos of a batch or playlist download at the same time: 1 (the default), 2, or 3. Many short videos finish much sooner, because each spends a few seconds starting before any data moves.\n\n" +
		"With more than one, the progress bar shows the whole queue, the status says how many are downloading, and each log line starts with its video's place in the queue, e.g. `[2/5]`. **Cancel** stops the whole queue; use **Skip** or **Pause** on a video's row in the Queue panel to stop just that one.\n\n" +
		"Downloading several at once makes YouTube's \"confirm you're not a bot\" check more likely. GoVid starts the downloads a few seconds apart, and when a site answers \"Too Many Requests\" (HTTP 429) or asks for the bot check, it goes back to one at a time for the rest of the session and says so in the log."},
	{"Pause and Resume", "**Pause** (beside Cancel, and on the downloading row of the Queue panel) stops the download but keeps what it has downloaded so far; the queue moves on to the next video. When only paused videos are left, the button reads **Resume**, which continues them all from where they stopped. **Cancel** or **Skip** still deletes the partly downloaded files.\n\n" +
		"Interrupted downloads also continue rather than start over: an automatic retry after a network error, a **Retry** of a failed or skipped video in the same session, and downloads GoVid was closed during. When you close GoVid with videos waiting or paused (including the one downloading), it saves them, and at the next start asks whether to **Resume** them or **Discard** them, which deletes their partial files. A resumed video is checked again first; if the site now offers different formats, its partial download starts over, and the log says so.\n\n" +
		"Live recordings cannot be paused; see **Live Streams**."},
	{"Live Streams", "When a URL is a stream that is live now, GoVid asks how to record it:\n" +
		"  * **Record from now** – records until you press **Stop recording** (the Cancel button, or Skip in the Queue panel) or the stream ends\n" +
		"  * **Record from the start** (YouTube and Twitch) – records the stream from its beginning\n" +
		"  * **Skip**\n\n" +
		"For a scheduled stream or premiere it says when it starts and offers **Wait and record**: GoVid checks every one to five minutes, counting down in the status line, and records once it starts.\n\n" +
		"While recording, the progress bar moves back and forth and the status shows the time and size recorded so far, e.g. \"Recording 00:12:34 · 410 MiB\". **Stop recording** keeps everything recorded: the file is saved in your chosen format (MKV when that format cannot hold the stream, such as WebM), added to the history, and post-processed like any download. Quitting GoVid while recording keeps the recording too.\n\n" +
		"A live stream has no known size, so the disk space check is skipped; instead GoVid checks the free space every 30 seconds and stops the recording, keeping it, when less than 1 GB is left. For a stream that has just ended and is still being processed, GoVid warns that only part of it may be available yet."},
	{"Save Destination", "The folder where the downloaded file will be saved. GoVid remembers this between sessions.\n\nBefore each download, GoVid checks that the folder's drive has room for it (with a 10% margin, and twice that with post-processing, which writes a second copy). If it does not, you can continue anyway or cancel; in a batch you can also skip that video. This includes each video picked from a playlist, which GoVid checks just before downloading it. When the site does not say how big a video is, the check is skipped."},
	{"Presets", "A preset is a named set of settings, such as an audio-only setup or a 1080p MP4 setup. Choosing one in the **Preset** dropdown sets the settings it holds and leaves every other setting as it is. GoVid starts with three: **Audio (MP3, metadata + cover)**, **1080p MP4**, and **Archive (MKV, Best, subtitles, chapters)**.\n\n" +
		"**(modified)** appears beside the dropdown once one of the preset's settings has been changed since it was chosen.\n\n" +
		"The **⋮** button beside the dropdown offers:\n" +
		"  * **Save current as preset…** – stores the current values of the groups you tick (format, quality, and preferred codec, save folder, speed limit, embedding, subtitles, the main window's toggles, post-processing, cookies) under a name. Using an existing name replaces that preset\n" +
		"  * **Manage presets…** – rename or delete presets\n" +
		"  * **Import presets…** / **Export presets…** – move presets between computers in one file. Imported presets replace presets of the same name; a value that does not work on this computer, such as a save folder that does not exist, is left out and listed"},
	{"Output Format", "The container format for the downloaded file:\n" +
		"  * **" + formatMP4 + "** – widely compatible, recommended for most uses\n" +
		"  * **" + formatMKV + "** – flexible container, ideal for high-quality archiving\n" +
		"  * **" + formatWebM + "** – open format, good for web use\n" +
		"  * **" + formatMP3 + "** – audio only, compressed\n" +
		"  * **" + formatM4A + "** – audio only, Apple/iTunes compatible"},
	{"Max Quality", "Sets the maximum resolution yt-dlp will request:\n" +
		"  * **" + qualityBest + "** – downloads the highest resolution available\n" +
		"  * **" + strings.Join(qualityOptions[1:], "** / **") + "** – caps the resolution to save space or bandwidth\n\n" +
		"A capped download is named after the resolution it actually got, e.g. `_720p`. When a video is not available at the cap, GoVid says so in the log and in a notice above the input card: it downloads the best version below the cap, or, when the site has no version at or below it, the best version there is, with a warning. Audio formats ignore this setting and get no label."},
	{"Formats", "**Formats…** (next to the URL field, for one URL; and on each waiting row of the Queue panel) lists every format the site offers: resolution, frame rate, HDR, video and audio codec, bitrate, container, and size (`~` marks an estimate). **Show** narrows it to video only, audio only, or video + audio. Thumbnail sheets (storyboards) are left out.\n\n" +
		"● marks what your **Output Format** and **Max Quality** would download. Click a video row and an audio row, or one row with both, and **Use these formats** downloads exactly those; **Automatic** goes back to letting the settings choose. The file still becomes your Output Format (remuxed or converted as usual).\n\n" +
		"Before each download the log says what it will fetch, e.g. \"Will download 401+251: 2160p AV1 + Opus → MP4 (~232.5 MiB)\". **Preferred Video Codec** (**Tools → Preferences**: Any, H.264, VP9, or AV1) picks that codec whenever a video offers it, even over a sharper version in another codec; H.264 plays on almost every device."},
	{"Filename Template", "Found in **Tools → Preferences**. How downloaded files are named, in yt-dlp's output template syntax, without the extension. The default, `GoVid_%(title)s{quality}`, gives GoVid's usual names. For example:\n" +
		"  * `%(uploader)s - %(title)s` → `Rick Astley - Never Gonna Give You Up.mp4`\n" +
		"  * `%(upload_date>%Y-%m-%d)s %(title)s [%(id)s]` → `2009-10-25 Never Gonna Give You Up [dQw4w9WgXcQ].mp4`\n\n" +
		"`{quality}` adds the height of a capped download, e.g. `_720p` (nothing for Best Quality or audio). A trimmed download still gets `_TRIM`. Below the field, a preview shows the name a sample video would get; fields the preview does not know are shown as written and filled in by yt-dlp (or written as `NA` when a site does not have them). **Reset** brings back the default.\n\n" +
		"The template cannot be empty or hold `/` or `\\`: files are saved directly in the save folder. Without `%(title)s` or `%(id)s`, every download gets the same name plus a number, so GoVid warns."},
	{"Trim Start / Trim End", "Download only a segment of the video. Leave both blank to download the full video.\n\nAccepted formats:\n  * `HH:MM:SS` (e.g. 01:30:00)\n  * `MM:SS` (e.g. 01:30)\n  * `Seconds` (e.g. 90)\n\nEither field can be used alone:\n  * **Trim Start only** → downloads from that point to the end\n  * **Trim End only** → downloads from the start to that point"},
	{"Save output to log file", "When checked, everything printed in the Terminal Output panel is also saved to a **GoVid_log_YYYY-MM-DD.txt** file in your save destination folder. Errors are also mirrored to a separate **GoVid_errors_YYYY-MM-DD.txt** file."},
	{"Notify on Completion", "When checked, a system notification is sent when a download finishes (success or failure), but not when cancelled."},
	{"Log Buffer Limit", "Found in **Tools → Preferences**. The number of lines kept in the Terminal Output panel; older lines are removed from the top. The panel never shows more than the latest **5000** lines, even with **Unlimited**. The limit does not apply to the log file, which is never trimmed. If you scroll up while a download is running, the panel stays where you left it; scroll back to the bottom to follow new lines again."},
	{"Embed in File", "Found in **Tools → Preferences**. What yt-dlp writes into each downloaded file:\n  * **Metadata** (on by default) – title, artist, upload date, and description tags, so music players show more than a file name\n  * **Thumbnail** (on by default) – the video's thumbnail as cover art, converted to JPEG. WebM files cannot hold cover art, so it is skipped for them\n  * **Chapters** – the video's chapter markers\n\nPost-processing keeps the cover art, chapters, subtitles, and tags of the files it re-encodes."},
	{"Subtitles", "Found in **Tools → Preferences**. Downloads the video's subtitles:\n" +
		"  * **" + subtitlesOff + "** (default) – no subtitles\n" +
		"  * **" + subtitlesEmbed + "** – as a subtitle track inside the video, which players let you turn on and off\n" +
		"  * **" + subtitlesSRT + "** – as `.srt` files next to the video, named like it with the language added, e.g. `GoVid_Title.en.srt`\n" +
		"  * **" + subtitlesBoth + "** – both of the above\n\n" +
		"**Subtitle Languages** takes language codes or patterns separated by commas, as yt-dlp's `--sub-langs` does: `en.*` (the default) matches `en`, `en-US`, `en-GB`, and so on; `all,-live_chat` takes every language except live chat. Before each download, the log lists the languages the video has and warns when none matches; the video then downloads without subtitles.\n\n" +
		"**Include auto-generated** also takes captions the site generated automatically, for languages that have no subtitles written by people. They are often inaccurate, and YouTube offers them in over 150 languages.\n\n" +
		"Audio formats cannot hold subtitles, so none are downloaded for them. WebM can only hold WebVTT subtitles, so those embedded in WebM stay in that format. For a trimmed download, subtitles still cover the whole video. If the subtitles cannot be downloaded (YouTube sometimes refuses with \"Too Many Requests\"), GoVid downloads the video again without them and says so."},
	{"Download History", "**File → History** lists your downloads, newest first, with each video's title, date, format and quality, and file name. Type in the search field to filter by title, URL, or file name. Each entry has:\n" +
		"  * **Re-add** – puts the URL back in the URL field (switching on Batch Mode if the field already holds a URL)\n" +
		"  * **Show in folder** – opens the folder with the file selected\n" +
		"  * **Copy URL** – copies the URL to the clipboard\n\n" +
		"Entries whose file has been moved or deleted are greyed out.\n\n" +
		"Before downloading, GoVid checks the history and asks before downloading a video again: **Download again** or **Skip**, and in a batch also **Skip all duplicates**. It recognises a video under a different link too, such as `youtu.be/…` for a `watch?v=…` link it downloaded before.\n\n" +
		"To stop keeping history, untick **Keep download history** in **Tools → Preferences**; GoVid then offers to delete the history kept so far. **Clear History** in the History window deletes it at any time."},
	{"Debug Output", "Found in **Tools → Preferences**. GoVid runs yt-dlp in verbose mode so the log file has everything needed for a bug report, but the **[debug]** lines are hidden from the Terminal Output panel unless this is checked. The panel also shows only the latest download progress line for each file; the log file keeps them all.\n\nWith Debug Output on, GoVid also writes a heartbeat to the log every 10 seconds: how long the window took to answer (a frozen window shows up as a large number, or as \"has not answered\"), how many background tasks and yt-dlp and FFmpeg processes are running, and how many log lines wait to be shown. Background loops log when they start and stop. This helps when GoVid freezes: turn it on, reproduce the freeze, and send the log or **Help → Copy diagnostics**."},
	{"Copy Diagnostics", "**Help → Copy diagnostics** puts a report for a bug report on the clipboard, and **Save as file…** saves it: GoVid's version, your system, the versions of yt-dlp, FFmpeg, and the JavaScript runtime, the GPU check, every setting, the queue, and the last 200 log lines.\n\nYour user folder and user name are replaced with `%USERPROFILE%` and `%USERNAME%`, and cookies appear only as their source (\"Firefox\", \"file set\"), never the file's path or contents. Look through the report before you post it anywhere public: it still holds the titles and links of what you downloaded."},
	{"Updates", "Found in **Tools → Preferences**. When **Check for updates on startup** is checked, GoVid asks GitHub (at most once a day) whether a newer yt-dlp is available and, if so, shows a notice with an **Update now** button. Sites change often, and an outdated yt-dlp is the most common reason downloads stop working. **Tools → Update yt-dlp** shows the installed and latest versions and updates on demand.\n\nGoVid also tells you when a newer GoVid release is available; **What's new** shows its release notes and a link to the download page. **Tools → Check for GoVid updates** checks right away.\n\nOn Windows, the release notes also offer **Update now**: GoVid downloads the new release, checks it against the SHA-256 published with it, replaces its own program file (keeping the old one until the next start), and restarts. If the download does not match, nothing is changed. Antivirus software or Windows SmartScreen may scan the new GoVid.exe the first time it starts, which can make that start slow or ask you to confirm it. **Update now** is not offered while a download runs, for releases published without a checksum, or for builds you made yourself.\n\nIf the update fails because GoVid's folder cannot be written to (for example under `Program Files`), run GoVid as administrator once, or move it to a folder you own."},
	{"Components", "**Tools → Components** lists the tools GoVid uses, the version installed and where it was found (the `bin` folder beside GoVid, which takes precedence, or `PATH`), and the latest version:\n" +
		"  * **yt-dlp** – downloads the videos\n" +
		"  * **FFmpeg (with ffprobe)** – merges video and audio, converts formats, and post-processes. Installing it also adds ffprobe, which lets post-processing show a percentage\n" +
		"  * **Deno** – a JavaScript runtime. YouTube makes yt-dlp solve a puzzle in JavaScript before it lists every format; without a runtime, yt-dlp falls back to an older YouTube client that may miss formats and will stop working when YouTube removes it\n\n" +
		"Each row has one button: **Install** (when the tool is missing, or found only on `PATH`), **Update** (when a newer version exists), or **Reinstall**, which repairs a broken copy. For yt-dlp, **Update** runs `yt-dlp -U`. Every download is checked against the SHA-256 its source publishes (yt-dlp and Deno from their GitHub releases, FFmpeg from gyan.dev), and if anything goes wrong the old tool is kept. Tools cannot be installed while a download runs.\n\n" +
		"GoVid also looks for Deno, Node.js (22 or newer), or Bun on `PATH`, and uses the first one yt-dlp supports; **Help → About** and the session log say which. When yt-dlp, FFmpeg, or a JavaScript runtime is missing, a notice at startup offers to install it.\n\n" +
		"On Linux the downloads do not apply; install the tools with your package manager."},
	{"Portable Mode", "Found in **Tools → Preferences**. Normally GoVid keeps its settings and presets in your user profile, so a copy of GoVid on a USB stick does not take them along, and two copies on one computer share them. With **Portable Mode** on, they are kept in `settings.json` beside `GoVid.exe` instead, so the whole GoVid folder carries them.\n\n" +
		"Turning it on or off copies your current settings and presets to the other place and takes effect when GoVid restarts (it offers to restart). It creates or deletes a small `GoVid.portable` file beside `GoVid.exe`; that file is what makes a copy portable. If GoVid's folder cannot be written to (for example under `Program Files`), Portable Mode cannot be turned on, and a portable copy started there keeps its settings in your user profile and says so.\n\n" +
		"What lives where:\n" +
		"  * **Beside GoVid.exe, always**: `download_history.json` (history), `queue.json` (downloads saved when you quit), and the `bin` folder (yt-dlp, FFmpeg, Deno)\n" +
		"  * **Beside GoVid.exe in Portable Mode, else in your user profile**: settings, presets, and the cached update checks\n" +
		"  * **In the save folder**: the log files"},
	{"Keyboard Shortcuts", "  * **Ctrl+Enter** – start the download (as **Download Now!**)\n" +
		"  * **Ctrl+O** – open the save folder\n" +
		"  * **Ctrl+L** – load URLs from a file\n" +
		"  * **Ctrl+Shift+V** – paste URLs (as the paste button)\n" +
		"  * **Ctrl+H** – History\n" +
		"  * **Ctrl+,** – Preferences\n" +
		"  * **F1** – this guide\n" +
		"  * **Esc** – close About, this guide, Preferences, Post-Processing, History, or Components\n\n" +
		"The Ctrl shortcuts work everywhere in the main window, also while typing in the URL field; the menus show them too. **F1** and **Esc** work when no text field has the cursor: click outside the field first (on macOS, use Cmd instead of Ctrl)."},
	{"Application Theme", "Found in **Tools → Preferences**. **System** (the default) follows your computer: light when Windows is set to light apps, dark otherwise, and GoVid switches along with it. **Dark** and **Light** keep that look whatever the system uses."},
	{"Save Preferences", "Found in **Tools → Preferences**. When checked, GoVid remembers your format, quality, save path, speed limit, and theme between sessions. The toggle itself is always remembered so the choice survives a restart."},
	{"Max Download Speed", "Found in **Tools → Preferences**. Limits the bandwidth used by GoVid to prevent network saturation. Examples:\n  * `50K` – Very slow\n  * `5M` – Moderate (standard HD streaming speed)\n  * `10G` – Virtually unlimited\n\nLeave blank to use full available bandwidth."},
	{"Cookies", "Found in **Tools → Preferences**. Cookies are your login: with them, yt-dlp can download age-restricted, members-only, and private videos you can watch when signed in, and get past YouTube's \"Sign in to confirm you're not a bot\" check. Choose where they come from:\n" +
		"  * **" + cookieSourceNone + "** (default) – no login\n" +
		"  * **" + cookieSourceFile + "** – a `cookies.txt` file in Mozilla/Netscape format, exported with a browser extension. It stops working when the site logs you out, so export it again then\n" +
		"  * **" + cookieSourceBrowser + "** – read straight from a browser you are signed in with; optionally name a profile (leave it empty for the default one)\n\n" +
		"On Windows, **Firefox** works best. Chrome, Edge, and other Chromium browsers lock their cookies while they are open, and encrypt them in a way yt-dlp cannot read (app-bound encryption). When that happens, GoVid says so in the log and suggests closing the browser, using Firefox, or exporting a `cookies.txt` instead. When a site asks you to sign in, or a video is age-restricted, members-only, or private, the log says which setting to use.\n\n" +
		"⚠️ **Security**: cookies give access to your accounts. GoVid only passes them to yt-dlp, which sends each cookie only to its own site. The session log names only the source (\"Firefox\", \"file set\"), never the file's path or its contents: where yt-dlp prints the path, the log shows `<cookies file>`. Never share a cookies file."},

	{"Post-Processing", "Found in **Tools → Post-Processing**. Enhance your downloads using FFmpeg. Most filters trigger a full re-encode.\n\n⚠️ **WebM files** use VP9 encoding which is significantly slower than H.264 — use MKV for faster post-processing."},
	{"Cancel", "Stops the active download immediately and deletes its partly downloaded files (use **Pause** to keep them). In batch mode, it skips the current URL and moves on to the next one. While a live stream is recorded, the button reads **Stop recording** and keeps what was recorded."},
	{"Open Folder", "Opens your chosen save destination in the system file manager."},
	{"JSON Configuration", "Every setting can be stored in a JSON file. **Tools → Export settings…** saves all of your current settings, and **Tools → Import settings…** applies a saved file, for example on another computer. A file named `govid.json` in the application folder is applied by **Load from Config** in **Tools → Preferences**.\n\n" +
		"A key left out of the file leaves that setting unchanged, so a file can hold only the settings you want to set. A value that is not allowed is skipped, and GoVid lists every skipped value.\n\n**Keys and values:**\n" +
		"* **path**: an existing folder\n" +
		"* **format**: " + codeList(formatOptions) + "\n" +
		"* **quality**: " + codeList(qualityOptions) + "\n" +
		"* **maxSpeed**: a rate with unit, e.g. `50K`, `5M`, `1G`, or `\"\"` for unlimited\n" +
		"* **cookiesPath**: an existing cookies file, or `\"\"` for none\n" +
		"* **filenameTemplate**: yt-dlp's output template without the extension, plus `{quality}`; no `/` or `\\`\n" +
		"* **simultaneousDownloads**: " + codeList(simultaneousOptions) + "; **preferredCodec**: " + codeList(preferredCodecOptions) + "\n" +
		"* **cookieSource**: " + codeList(cookieSourceOptions) + "; **cookieBrowser**: " + codeList(cookieBrowserOptions) + "; **cookieProfile**: a browser profile name, or `\"\"` for the default\n" +
		"* **themeMode**: " + codeList(themeOptions) + "\n" +
		"* **logLimit**: " + codeList(logLimitOptions) + "\n" +
		"* **subtitles**: " + codeList(subtitleModeOptions) + "\n" +
		"* **subtitleLangs**: yt-dlp `--sub-langs` syntax, e.g. `en.*,de`\n" +
		"* **smoothMotionMode**: " + codeList(smoothModeOptions) + "\n" +
		"* **smoothFPS**: 24 to 120; **sharpenAmount**: 0 to 2\n" +
		"* **denoiseMode**: " + codeList(denoiseModeOptions) + "\n" +
		"* **upscaleTarget**: " + codeList(upscaleTargetOptions) + "\n" +
		"* **gpuBackend**: " + codeList(GPUBackendOptions()) + "\n" +
		"* `true` or `false`: **savePrefs**, **showDebug**, **checkUpdates**, **embedMetadata**, **embedThumbnail**, **embedChapters**, **autoSubtitles**, **keepHistory**, **batchMode**, **saveLog**, **notify**, **autoRetry**, **postProcess**, **smoothMotion**, **sharpen**, **normalizeAudio**, **vividMode**, **denoise**, **hdrToSdr**, **deband**, **autoCrop**, **stabilize**, **deinterlace**, **nightMode**, **upscaleVideo**"},
}

// showConfigHelp opens a scrollable window explaining all configuration
// options (helpItems). It is a singleton: if already open, the existing
// window is focused instead.
func (manager *UIManager) showConfigHelp() {
	if focusOrCreate(&manager.helpWindow) {
		return
	}

	content := container.NewVBox()
	for _, item := range helpItems {
		title := widget.NewRichTextFromMarkdown("### " + item.label)
		title.Wrapping = fyne.TextWrapOff

		content.Add(title)
		content.Add(newHelpBody(item.desc))
		content.Add(widget.NewSeparator())
	}

	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(520, 420))

	manager.helpWindow = fyne.CurrentApp().NewWindow("GoVid Guide")
	manager.helpWindow.SetContent(container.NewPadded(scroll))
	manager.helpWindow.Resize(fyne.NewSize(550, 500))
	manager.helpWindow.SetOnClosed(onWindowClosed(&manager.helpWindow))
	closeOnEscape(manager.helpWindow)
	manager.helpWindow.Show()
}

// newHelpBody renders a help topic's Markdown text, wrapped, with bold text
// in the primary colour and code in the warning colour, so names of
// settings and values stand out.
func newHelpBody(markdown string) *widget.RichText {
	body := widget.NewRichTextFromMarkdown(markdown)
	for segIdx := range body.Segments {
		if segment, ok := body.Segments[segIdx].(*widget.TextSegment); ok {
			if segment.Style.TextStyle.Bold {
				segment.Style.ColorName = theme.ColorNamePrimary
			}
			if segment.Style.TextStyle.Monospace {
				segment.Style.ColorName = theme.ColorNameWarning
			}
		}
	}
	body.Wrapping = fyne.TextWrapWord
	return body
}

// codeList renders options as a comma-separated list of Markdown code spans,
// e.g. "`MP4`, `MKV`".
func codeList(options []string) string {
	return "`" + strings.Join(options, "`, `") + "`"
}
