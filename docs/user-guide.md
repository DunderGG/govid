# GoVid User Guide

This guide covers everything GoVid can do, from a first download to post-processing and configuration files. For a short introduction, see the [README](../README.md). GoVid has the same guide built in: press **F1** or open **Help → GoVid Guide**.

## Contents

- [Getting started](#getting-started)
  - [Installing on Windows](#installing-on-windows)
  - [Running on Linux](#running-on-linux)
  - [Your first download](#your-first-download)
  - [The main window](#the-main-window)
- [Adding videos](#adding-videos)
  - [Batch Mode and URL lists](#batch-mode-and-url-lists)
  - [Playlists](#playlists)
  - [Live streams](#live-streams)
- [Managing downloads](#managing-downloads)
  - [The Queue panel](#the-queue-panel)
  - [Simultaneous downloads](#simultaneous-downloads)
  - [Pause, resume, and restarts](#pause-resume-and-restarts)
  - [Cancel and Auto-retry](#cancel-and-auto-retry)
  - [Disk space check](#disk-space-check)
- [Choosing what to download](#choosing-what-to-download)
  - [Output format](#output-format)
  - [Max Quality](#max-quality)
  - [Format browser and preferred codec](#format-browser-and-preferred-codec)
  - [Trimming](#trimming)
- [What you get](#what-you-get)
  - [File names](#file-names)
  - [Metadata, cover art, and chapters](#metadata-cover-art-and-chapters)
  - [Subtitles](#subtitles)
- [Post-processing](#post-processing)
  - [Filters](#filters)
  - [GPU-accelerated encoding](#gpu-accelerated-encoding)
- [Sign-in cookies](#sign-in-cookies)
- [Download history](#download-history)
- [Presets](#presets)
- [Preferences](#preferences)
  - [Portable Mode](#portable-mode)
  - [Where GoVid keeps its files](#where-govid-keeps-its-files)
- [Configuration files (govid.json)](#configuration-files-govidjson)
- [Tools and updates](#tools-and-updates)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Command line](#command-line)
- [Troubleshooting](#troubleshooting)

---

## Getting started

### Installing on Windows

1. Download `GoVid_<version>_Ready.zip` from the [latest release](https://github.com/DunderGG/govid/releases/latest).
2. Extract it to a folder you own, for example in your user folder, not under `Program Files`. GoVid writes its history and tools beside `GoVid.exe`, and updates itself in place, so its folder must be writable.
3. Run `GoVid.exe`.

The zip includes `yt-dlp` and `ffmpeg` in a `bin` folder. At first start, GoVid offers to install anything that is missing, most likely Deno. Deno is a JavaScript runtime that yt-dlp needs to list every YouTube format. You can also install, update, or repair each tool later from [**Tools → Components**](#tools-and-updates).

### Running on Linux

There is no Linux release build yet. [Build GoVid from source](../README.md#-building-from-source), then install `yt-dlp`, `ffmpeg`, and a JavaScript runtime ([Deno](https://deno.com/) 2.3 or newer, or Node.js 22 or newer) with your package manager. GoVid finds them on `PATH`. **Tools → Components** shows what it found, but its installer is for Windows only.

### Your first download

1. Paste a video link into **Video URL**. Any site [supported by yt-dlp](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md) works.
2. Choose an **Output Format** (MP4 is the safest choice) and a **Max Quality**.
3. Choose the **Save Destination** folder.
4. Click **Download Now!** or press **Ctrl+Enter**.

Progress shows in the status line, the progress bar, and the **Terminal Output** log. When the download finishes, **Open Folder** opens the save folder.

### The main window

| Control | What it does |
| :--- | :--- |
| **Video URL** | The link to download. Beside it: **Load from file…**, the paste button, the clear button, and **Formats…** |
| **Preset** | Applies a saved set of settings. See [Presets](#presets) |
| **Output Format** / **Max Quality** | The file type and the highest resolution to download |
| **Trim Start** / **Trim End** | Download only part of a video. See [Trimming](#trimming) |
| **Save Destination** | The folder downloads go to. **Open Folder** opens it |
| **Batch Mode** | Turns the URL field into a list, one URL per line |
| **Save output to log file** | Also writes the log to a file in the save folder |
| **Notify on Completion** | Shows a desktop notification when a download finishes or fails (not when you cancel it) |
| **Auto-retry** | Retries a download after a temporary error. See [Cancel and Auto-retry](#cancel-and-auto-retry) |
| **Post-Processing** | Turns the filters chosen in **Tools → Post-Processing** on or off |
| **Download Now!** / **Pause** / **Cancel** | Start, pause, or stop the downloads |
| **Terminal Output** | The log of everything GoVid and yt-dlp do |

If you scroll up in the log while a download runs, it stays where you left it. Scroll back to the bottom to follow new lines again. **File → Clear Terminal Output** empties it.

---

## Adding videos

### Batch Mode and URL lists

Turn on **Batch Mode** to paste several URLs, one per line. Lines starting with `#` are comments and are not downloaded.

Other ways to add URLs:

- **Load from file…** (**Ctrl+L**) reads a `.txt` file with one URL per line. Blank lines, lines starting with `#`, and lines that are not links are skipped, and the log says which ones were skipped.
- **The paste button** next to the URL field (**Ctrl+Shift+V**) adds the links on the clipboard, one per line. If anything on the clipboard is not a link, nothing is added.
- **Drop a file onto the window**: a `.txt` list or an internet shortcut (`.url`). On Windows, a link dragged straight from a browser does not arrive. Drag it to the desktop first, then drop the shortcut that creates.

Each of these switches on Batch Mode when the field then holds more than one URL, and skips URLs that are already in the field.

### Playlists

GoVid checks each URL before it downloads it. When a URL is a playlist, GoVid shows the playlist's title, number of videos, and total length, and asks which videos you want:

- Leave the range blank and click **Download** to get them all.
- Enter a range such as `1-10`, `5-`, or `3,5,8` to get only those videos.
- For a link to one video inside a playlist (`watch?v=…&list=…`), **Only this video** is selected by default.

Each chosen video becomes its own item in the [queue](#the-queue-panel), with its own progress, controls, and history entry.

### Live streams

When a URL points to a stream that is live now, GoVid asks how to record it:

- **Record from now** records until you press **Stop recording** or the stream ends.
- **Record from the start** (YouTube and Twitch) records the stream from its beginning.
- **Skip** skips the stream.

For a scheduled stream or premiere, GoVid shows when it starts and offers **Wait and record**. It then checks every one to five minutes, counts down in the status line, and starts recording when the stream begins.

While GoVid records, the status line shows the time and size recorded so far, for example "Recording 00:12:34 · 410 MiB". The **Cancel** button becomes **Stop recording**, which keeps everything recorded so far. The recording is saved in your chosen format, or as MKV when that format cannot hold the stream (WebM, for example). GoVid adds it to the history and post-processes it like any other download. If you quit GoVid while it records, the recording is kept too.

A live stream has no known size, so the [disk space check](#disk-space-check) cannot run before it starts. Instead, GoVid checks the free space every 30 seconds and stops the recording, keeping the file, when less than 1 GB is left.

Live recordings cannot be paused. For a stream that has just ended and is still being processed by the site, GoVid warns that only part of it may be available yet.

---

## Managing downloads

### The Queue panel

When there is more than one video to download, through Batch Mode or a playlist, the **Queue** panel appears above the log. It lists each video with its status: Waiting, Checking, Downloading (with a percentage), Paused, Post-processing, Done, Failed, or Skipped. Its title counts progress, for example "7 of 20 done, 1 failed". Click the title to fold the panel away.

While the queue runs, you can:

- move **waiting** videos up or down (the list order is the download order), remove them, or open **Formats…** to choose their formats;
- **Pause** or **Skip** the video that is downloading, and the queue moves on to the next one;
- **Resume** a paused video, which moves it to the front of the waiting videos and continues where it stopped, or remove it, which deletes what it had downloaded;
- **Retry** a video that failed or was skipped, which puts it back at the end of the queue.

If you stop the queue, the videos it did not get to are marked Skipped.

### Simultaneous downloads

**Tools → Preferences → Simultaneous Downloads** sets how many videos of a batch or playlist download at the same time: 1 (the default), 2, or 3. A list of many short videos finishes much sooner this way, because each video spends a few seconds starting before any data arrives.

With more than one at a time, the progress bar shows the whole queue, and each log line starts with its video's place in the queue, for example `[2/5]`. **Cancel** stops the whole queue. To stop a single video, use **Skip** or **Pause** on its row in the Queue panel.

Several downloads at once make YouTube's "confirm you're not a bot" check more likely. GoVid starts the downloads a few seconds apart. If a site answers "Too Many Requests" (HTTP 429) or asks for the bot check, GoVid goes back to one download at a time for the rest of the session and says so in the log.

### Pause, resume, and restarts

**Pause** (beside **Cancel**, and on the downloading row of the Queue panel) stops a download but keeps what it has downloaded so far. When only paused videos are left, the button reads **Resume**, which continues them all from where they stopped.

Interrupted downloads also continue instead of starting over. This happens after an automatic retry, when you **Retry** a failed or skipped video in the same session, and after you close GoVid during a download. When you close GoVid with videos waiting or paused, it saves them. At the next start it asks whether to **Resume** them or **Discard** them, which deletes their partial files. A resumed video is checked again first. If the site now offers different formats, its download starts over, and the log says so.

### Cancel and Auto-retry

**Cancel** stops the current download immediately and deletes its partial files. Use **Pause** instead to keep them. In a batch, Cancel skips the current URL and moves on to the next.

With **Auto-retry** on, GoVid retries a download that fails with a temporary error, such as a dropped connection, up to three attempts in total. It waits a little longer before each retry, and the retry continues from where the download stopped.

### Disk space check

Before each download, GoVid checks that the save folder's drive has room for it. It adds a 10% margin, or 20% with post-processing, which writes a second copy of the file. If there is not enough space, you can continue anyway or cancel, and in a batch you can also skip that video. Videos picked from a playlist are each checked just before they download. When a site does not say how big a video is, the check is skipped.

---

## Choosing what to download

### Output format

| Format | Use it for |
| :--- | :--- |
| **MP4** | Video that plays almost everywhere. The best default |
| **MKV** | Archiving. A flexible container that holds any codec, subtitles, and chapters |
| **WebM** | Open-format video for the web. Slow to post-process, see [Post-processing](#post-processing) |
| **MP3** | Audio only, widely supported |
| **M4A** | Audio only, for Apple devices and iTunes |

GoVid remuxes or converts the download to the format you choose.

### Max Quality

**Max Quality** sets the highest resolution GoVid downloads: **Best Quality**, **1080p**, **720p**, **480p**, or **360p**. A lower cap saves space and bandwidth.

A capped download is named after the resolution it actually got, for example `_720p`. When a video is not available at the cap, GoVid says so in the log and in a notice above the input card. It then downloads the best version below the cap. If the site has no version at or below the cap, GoVid downloads the best version available and warns you. Audio formats ignore this setting.

### Format browser and preferred codec

**Formats…** (next to the URL field, and on each waiting row of the Queue panel) lists every format the site offers. For each one it shows the resolution, frame rate, HDR, video and audio codec, bitrate, container, and size (`~` marks an estimate). **Show** narrows the list to video only, audio only, or video with audio.

● marks what your **Output Format** and **Max Quality** would download. To choose for yourself, click a video row and an audio row, or one row that has both, then **Use these formats**. **Automatic** goes back to letting your settings choose. Either way, the file is still converted to your Output Format.

Before each download, the log says what it will fetch, for example "Will download 401+251: 2160p AV1 + Opus → MP4 (~232.5 MiB)".

**Tools → Preferences → Preferred Video Codec** (Any, H.264, VP9, or AV1) makes GoVid pick that codec whenever a video offers it, even over a sharper version in another codec. H.264 plays on almost every device.

### Trimming

To download only part of a video, enter **Trim Start**, **Trim End**, or both. Leave both empty to download the whole video. Times can be written as `HH:MM:SS` (`01:30:00`), `MM:SS` (`01:30`), or seconds (`90`).

- **Trim Start only** downloads from that point to the end.
- **Trim End only** downloads from the beginning to that point.

Trimmed files get `_TRIM` added to their names.

---

## What you get

### File names

**Tools → Preferences → Filename Template** sets how downloads are named, using [yt-dlp's output template](https://github.com/yt-dlp/yt-dlp#output-template) syntax without the extension. The default, `GoVid_%(title)s{quality}`, gives names like `GoVid_Never Gonna Give You Up_720p.mp4`. More examples:

| Template | Result |
| :--- | :--- |
| `%(uploader)s - %(title)s` | `Rick Astley - Never Gonna Give You Up.mp4` |
| `%(upload_date>%Y-%m-%d)s %(title)s [%(id)s]` | `2009-10-25 Never Gonna Give You Up [dQw4w9WgXcQ].mp4` |

- `{quality}` is GoVid's own field. It adds the height of a capped download, for example `_720p`, and nothing for Best Quality or audio.
- A preview below the field shows the name a sample video would get. Fields the preview does not know are shown as written. yt-dlp fills them in, or writes `NA` when a site does not have them.
- The template cannot be empty or contain `/` or `\`, because files are saved directly in the save folder.
- Without `%(title)s` or `%(id)s`, every download would get the same name plus a number, so GoVid warns you.
- **Reset** brings back the default.

### Metadata, cover art, and chapters

**Tools → Preferences → Embed in File** chooses what is written into each downloaded file:

- **Metadata** (on by default): title, artist, upload date, and description tags, so music players show more than a file name.
- **Thumbnail** (on by default): the video's thumbnail as cover art, converted to JPEG. WebM files cannot hold cover art, so it is skipped for them.
- **Chapters**: the video's chapter markers.

Post-processing keeps the cover art, chapters, subtitles, and tags of the files it re-encodes.

### Subtitles

**Tools → Preferences → Subtitles** downloads a video's subtitles:

| Setting | Result |
| :--- | :--- |
| **Off** (default) | No subtitles |
| **Embed** | A subtitle track inside the video, which players can turn on and off |
| **Save as .srt** | `.srt` files next to the video, named with the language added, for example `GoVid_Title.en.srt` |
| **Both** | Both of the above |

**Subtitle Languages** takes language codes or patterns separated by commas, in the same syntax as yt-dlp's `--sub-langs`. The default `en.*` matches `en`, `en-US`, `en-GB`, and so on. `all,-live_chat` takes every language except live chat. Before each download, the log lists the languages the video has, and warns when none matches. The video then downloads without subtitles.

**Include auto-generated** also takes captions the site generated automatically, for languages that have no subtitles written by people. These are often inaccurate.

- MP3 and M4A cannot hold subtitles, so none are downloaded for them.
- WebM can only hold WebVTT subtitles, so subtitles embedded in WebM stay in that format.
- For a trimmed download, the subtitles still cover the whole video.
- If the subtitles cannot be downloaded (YouTube sometimes refuses with "Too Many Requests"), GoVid downloads the video again without them and says so.

---

## Post-processing

GoVid can run FFmpeg filters on each finished download. Choose them in **Tools → Post-Processing**, and turn them all on or off with the **Post-Processing** checkbox in the main window.

Most video filters re-encode the whole video, which can take much longer than the download itself. The **Estimated Processing Load** meter in the Post-Processing window shows how heavy your current selection is. With FFmpeg's `ffprobe` installed (**Tools → Components** installs it), post-processing shows a percentage.

Audio filters alone do not re-encode the video. Video filters are skipped for MP3 and M4A downloads.

> **Tip:** WebM uses VP9 encoding, which is much slower than H.264 and always runs on the CPU. Choose MKV or MP4 if you post-process often.

### Filters

**Motion**

| Filter | What it does |
| :--- | :--- |
| **Smooth Motion** | Interpolates new frames for more fluid playback. Slow |
| ↳ **Smoothing Mode** | **Precise (slow)** and **Balanced** use motion vectors. **Fast** blends frames |
| ↳ **Target FPS** | 24 to 120. 60 is standard, 24 is cinematic, 120 is for high-refresh screens |
| **Stabilize** | Smooths out shaky handheld footage (`deshake`) |
| **Deinterlace** | Removes combing artifacts from archival or TV-recorded video (`bwdif`) |

**Picture**

| Filter | What it does |
| :--- | :--- |
| **Vivid Mode** | Boosts brightness, contrast, and saturation |
| **Sharpen Video** | Sharpens edges without halos or extra noise (Contrast Adaptive Sharpening) |
| ↳ **Sharpen Intensity** | 0 to 2. 1.0 is gentle, 1.5 moderate, 2.0 strong |
| **Fix Banding** | Removes visible steps in smooth gradients, such as skies and dark scenes |
| **HDR to SDR** | Tone-maps HDR video (PQ or HLG) for standard monitors. SDR video is left unchanged |
| **Denoise** | Reduces noise in grainy or low-quality footage |
| ↳ **Denoise Mode** | **NLMeans (HQ, slow)** gives the best quality. **hqdn3d (Balanced)** is fast and effective |
| **Auto-Crop** | Detects and removes black letterbox or pillarbox bars |
| **Upscale Video** | Enlarges the video with a high-quality Lanczos resampler |
| ↳ **Target Resolution** | **2× (Double)** doubles both dimensions. **1080p**, **1440p**, and **4K (2160p)** set a fixed height |

**Audio**

| Filter | What it does |
| :--- | :--- |
| **Normalize Audio** | Evens out loudness (`loudnorm`) |
| **Night Mode** | Compresses the dynamic range so quiet dialogue and loud effects are closer in volume (`dynaudnorm`) |

Upscaling and Smooth Motion make files noticeably larger, and the Post-Processing window warns about this.

### GPU-accelerated encoding

**Encoder Backend** in the Post-Processing window chooses where the re-encode runs:

- **Auto (Recommended)** uses the first graphics card encoder that works on your computer, and the CPU otherwise.
- **Off** always uses the CPU.
- A specific backend: **NVIDIA** (NVENC, Windows and Linux), **Intel** (Quick Sync, Windows and Linux), **AMD** (AMF, Windows), or **VAAPI** (Linux). Only the backends that apply to your system are listed.

GoVid checks which backends work on your computer. If the one you chose is not available, it falls back to the CPU. WebM always uses the CPU, because the GPU encoders do not support VP9. The session log says which encoder each file used.

---

## Sign-in cookies

Some videos need you to be signed in, such as age-restricted, members-only, and private videos. YouTube may also ask you to "sign in to confirm you're not a bot". Cookies are your login: with them, yt-dlp can download what you can watch when signed in.

Choose the source in **Tools → Preferences → Cookies**:

- **None** (default): no login.
- **From file**: a `cookies.txt` file in Mozilla/Netscape format, exported with a browser extension. It stops working when the site logs you out, so export it again then.
- **From browser**: read directly from a browser you are signed in with. Optionally name a browser profile, or leave it empty for the default profile.

On Windows, **Firefox** works best. Chrome, Edge, and other Chromium browsers lock their cookies while they are open, and encrypt them in a way yt-dlp cannot read. When that happens, GoVid says so in the log and suggests closing the browser, using Firefox, or exporting a `cookies.txt` instead. When a site asks you to sign in, the log says which setting to use.

> **Security:** cookies give access to your accounts. GoVid passes them only to yt-dlp, which sends each cookie only to its own site. The log never shows a cookie file's path or contents, only the source ("Firefox", "file set"). Never share a cookies file.

---

## Download history

**File → History** (**Ctrl+H**) lists your downloads, newest first, with each video's title, date, format, quality, and file name. Type in the search field to filter by title, URL, or file name. Each entry has these buttons:

- **Re-add** puts the URL back in the URL field, switching on Batch Mode if the field already holds a URL.
- **Show in folder** opens the folder with the file selected.
- **Copy URL** copies the URL to the clipboard.

Entries whose file has been moved or deleted are greyed out.

Before downloading, GoVid checks the history and asks before downloading a video a second time: **Download again** or **Skip**, and in a batch also **Skip all duplicates**. It recognises a video under a different link too, such as a `youtu.be/…` link to a video it downloaded from a `watch?v=…` link.

To stop keeping history, untick **Download History** in **Tools → Preferences**. GoVid then offers to delete the history kept so far. **Clear History** in the History window deletes it at any time.

---

## Presets

A preset is a named set of settings, such as "audio only" or "1080p MP4". Choosing one in the **Preset** dropdown applies the settings it holds and leaves every other setting as it is. GoVid comes with three:

- **Audio (MP3, metadata + cover)**
- **1080p MP4**
- **Archive (MKV, Best, subtitles, chapters)**

**(modified)** appears beside the dropdown when you change one of the preset's settings after choosing it.

The **⋮** button beside the dropdown offers:

- **Save current as preset…** stores the current values of the groups you tick under a name: format, quality, and preferred codec; save folder; speed limit; embedding; subtitles; the main window's toggles; post-processing; and cookies. Using an existing name replaces that preset.
- **Manage presets…** renames or deletes presets.
- **Import presets…** and **Export presets…** move presets between computers in one file. Imported presets replace presets of the same name. A value that does not work on this computer, such as a save folder that does not exist, is left out and listed.

---

## Preferences

**Tools → Preferences** (**Ctrl+,**) holds the settings that are not in the main window. Most of them are described in the sections above.

| Setting | What it does |
| :--- | :--- |
| **Save Preferences** | Remembers your settings between sessions. On by default |
| **Log Buffer Limit** | How many lines the log panel keeps: 100 to 5000, or Unlimited. The panel never shows more than the latest 5000 lines. The log file is never trimmed |
| **Debug Output** | Shows yt-dlp's `[debug]` lines in the log panel. They always go to the log file. Also writes a heartbeat to the log every 10 seconds, which helps diagnose freezes (see [Troubleshooting](#troubleshooting)) |
| **Updates** | Checks GitHub once a day for newer yt-dlp and GoVid releases. See [Tools and updates](#tools-and-updates) |
| **Download History** | See [Download history](#download-history) |
| **Embed in File** | See [Metadata, cover art, and chapters](#metadata-cover-art-and-chapters) |
| **Subtitles** / **Subtitle Languages** | See [Subtitles](#subtitles) |
| **Portable Mode** | See [Portable Mode](#portable-mode) |
| **Filename Template** | See [File names](#file-names) |
| **Preferred Video Codec** | See [Format browser and preferred codec](#format-browser-and-preferred-codec) |
| **Simultaneous Downloads** | See [Simultaneous downloads](#simultaneous-downloads) |
| **Max Download Speed** | Caps the download bandwidth, for example `50K`, `5M`, or `10G`. Leave it empty for no limit |
| **Application Theme** | **System** (default) follows your computer's light or dark setting and switches along with it. **Dark** and **Light** stay fixed |
| **Cookies** | See [Sign-in cookies](#sign-in-cookies) |

At the bottom, **Restore Defaults** resets all preferences to their default values, and **Load from Config (govid.json)** applies a [configuration file](#configuration-files-govidjson).

### Portable Mode

Normally GoVid keeps its settings and presets in your user profile. That means a copy of GoVid on a USB stick does not take them along, and two copies on one computer share them.

With **Portable Mode** on, settings and presets are kept in `settings.json` beside `GoVid.exe` instead, so the whole GoVid folder carries them. Turning it on or off copies your current settings and presets to the other place, and takes effect when GoVid restarts (it offers to restart). What makes a copy portable is a small `GoVid.portable` file beside `GoVid.exe`, which GoVid creates or deletes.

If GoVid's folder cannot be written to (for example under `Program Files`), Portable Mode cannot be turned on. A portable copy started from such a folder keeps its settings in your user profile and says so.

### Where GoVid keeps its files

| Location | Files |
| :--- | :--- |
| Beside `GoVid.exe`, always | `download_history.json` (history), `queue.json` (downloads saved when you quit), and the `bin` folder (yt-dlp, FFmpeg, Deno) |
| Beside `GoVid.exe` in Portable Mode, otherwise in your user profile | Settings, presets, and the cached update checks |
| The save folder | Log files, when **Save output to log file** is on |

---

## Configuration files (govid.json)

Every setting can be stored in a JSON file. Use one to set up GoVid the same way on several computers, or to switch quickly between setups.

- **Tools → Export settings…** saves all your current settings to a file.
- **Tools → Import settings…** applies a saved file.
- A file named `govid.json` beside `GoVid.exe` is applied by **Load from Config (govid.json)** in **Tools → Preferences**.

A key left out of the file leaves that setting unchanged, so a file can hold only the settings you want to change:

```json
{
  "path": "C:\\Downloads\\YouTube",
  "format": "MP4",
  "quality": "1080p",
  "maxSpeed": "5M"
}
```

Some rules:

- Backslashes in Windows paths must be doubled, as in the example above. JSON treats a single `\` as the start of an escape sequence.
- JSON does not allow comments. A file with comments fails to load.
- A value that is not allowed is skipped, and GoVid lists every skipped value when it loads the file. The other values still apply.
- For a setting with fixed choices, `""` leaves it unchanged.
- Keys GoVid does not know are ignored, so a file exported from a newer GoVid still loads.

[govid.example.json](../govid.example.json) in the repository is an example with every key at its default value.

### Key reference

Defaults are in parentheses.

**Downloads**

| Key | Values |
| :--- | :--- |
| `path` | An existing folder (the application folder) |
| `format` | `MP4`, `MKV`, `WebM`, `MP3`, `M4A` (`MP4` on Windows and macOS, `MKV` elsewhere) |
| `quality` | `Best Quality`, `1080p`, `720p`, `480p`, `360p` (`Best Quality`) |
| `preferredCodec` | `Any`, `H.264`, `VP9`, `AV1` (`Any`) |
| `simultaneousDownloads` | `1`, `2`, `3` (`1`) |
| `maxSpeed` | A rate with a unit, such as `50K`, `5M`, `1G`; `""` for no limit (`""`) |
| `filenameTemplate` | yt-dlp's output template without the extension, plus `{quality}`; no `/` or `\` (`GoVid_%(title)s{quality}`) |

**Main window toggles**

| Key | Values |
| :--- | :--- |
| `batchMode`, `saveLog`, `notify`, `autoRetry` | `true` or `false` (all `false`) |
| `postProcess` | `true` or `false` (`true`) |

**Embedding and subtitles**

| Key | Values |
| :--- | :--- |
| `embedMetadata`, `embedThumbnail`, `embedChapters` | `true` or `false` (`true`, `true`, `false`) |
| `subtitles` | `Off`, `Embed`, `Save as .srt`, `Both` (`Off`) |
| `subtitleLangs` | Languages in yt-dlp's `--sub-langs` syntax, such as `en.*,de` (`en.*`) |
| `autoSubtitles` | `true` or `false`: also take auto-generated captions (`false`) |

**Cookies**

| Key | Values |
| :--- | :--- |
| `cookieSource` | `None`, `From file`, `From browser` (`None`) |
| `cookiesPath` | An existing `cookies.txt` file, or `""` for none (`""`). Used with `From file` |
| `cookieBrowser` | `Firefox`, `Chrome`, `Edge`, `Brave`, `Chromium`, `Opera`, `Vivaldi`, `Whale`, `Safari` (`Firefox`). Used with `From browser` |
| `cookieProfile` | A browser profile name, or `""` for the default profile (`""`) |

**Post-processing**

| Key | Values |
| :--- | :--- |
| `smoothMotion`, `stabilize`, `deinterlace`, `vividMode`, `sharpen`, `deband`, `hdrToSdr`, `denoise`, `autoCrop`, `upscaleVideo`, `normalizeAudio`, `nightMode` | `true` or `false` (all `false`) |
| `smoothMotionMode` | `Precise (slow)`, `Balanced`, `Fast` (`Balanced`) |
| `smoothFPS` | A number from 24 to 120 (`60`) |
| `sharpenAmount` | A number from 0 to 2 (`1`) |
| `denoiseMode` | `NLMeans (HQ, slow)`, `hqdn3d (Balanced)` (`hqdn3d (Balanced)`) |
| `upscaleTarget` | `2× (Double)`, `1080p`, `1440p`, `4K (2160p)` (`2× (Double)`) |
| `gpuBackend` | `Auto (Recommended)`, `Off`, or a backend the Post-Processing window offers on your system (`Auto (Recommended)`) |

**Application**

| Key | Values |
| :--- | :--- |
| `savePrefs` | `true` or `false`: remember settings between sessions (`true`) |
| `themeMode` | `System`, `Dark`, `Light` (`System`) |
| `logLimit` | `100`, `200`, `500`, `1000`, `5000`, `Unlimited` (`200`) |
| `showDebug` | `true` or `false` (`false`) |
| `checkUpdates` | `true` or `false` (`true`) |
| `keepHistory` | `true` or `false` (`true`) |

---

## Tools and updates

**Tools → Components** lists the tools GoVid uses. For each one it shows the installed version, where it was found (the `bin` folder beside GoVid, which takes precedence, or `PATH`), and the latest version.

- **yt-dlp** downloads the videos.
- **FFmpeg (with ffprobe)** merges video and audio, converts formats, and post-processes. Installing it also adds ffprobe, which lets post-processing show a percentage.
- **Deno** is a JavaScript runtime. YouTube makes yt-dlp solve a puzzle in JavaScript before it lists every format. Without a runtime, yt-dlp falls back to an older YouTube client that may miss formats.

Each row has one button: **Install** (when the tool is missing, or found only on `PATH`), **Update** (when a newer version exists), or **Reinstall**, which repairs a broken copy. Every download is checked against the SHA-256 checksum its source publishes, and if anything goes wrong, the old tool is kept. Tools cannot be installed while a download runs. On Linux, install the tools with your package manager instead.

GoVid also finds Deno, Node.js (22 or newer), or Bun on `PATH`, and uses the first one yt-dlp supports. **Help → About GoVid** and the session log say which.

**Updating yt-dlp.** Sites change often, and an outdated yt-dlp is the most common reason downloads stop working. With **Updates** on in Preferences, GoVid checks once a day and shows a notice with an **Update now** button when a newer yt-dlp is available. **Tools → Update yt-dlp** shows the installed and latest versions and updates on demand.

**Updating GoVid.** GoVid also tells you when a newer release is available. **What's new** shows its release notes and a link to the download page, and **Tools → Check for GoVid updates** checks right away. On Windows, the release notes also offer **Update now**: GoVid downloads the new release, checks it against the published SHA-256 checksum, replaces its own program file, and restarts. If the checksum does not match, nothing is changed. **Update now** is not offered while a download runs, for releases published without a checksum, or for builds you made yourself.

Antivirus software or Windows SmartScreen may scan the new `GoVid.exe` the first time it starts, which can make that start slow or ask you to confirm it. If the update fails because GoVid's folder cannot be written to, run GoVid as administrator once, or move it to a folder you own.

---

## Keyboard shortcuts

| Shortcut | Action |
| :--- | :--- |
| **Ctrl+Enter** | Start the download |
| **Ctrl+O** | Open the save folder |
| **Ctrl+L** | Load URLs from a file |
| **Ctrl+Shift+V** | Paste URLs |
| **Ctrl+H** | History |
| **Ctrl+,** | Preferences |
| **F1** | GoVid Guide |
| **Esc** | Close the current window (About, Guide, Preferences, Post-Processing, History, or Components) |

The Ctrl shortcuts work anywhere in the main window, also while you type in the URL field. **F1** and **Esc** work only when no text field has the cursor, so click outside the field first. On macOS, use Cmd instead of Ctrl.

---

## Command line

| Option | What it does |
| :--- | :--- |
| `--update` | Updates yt-dlp to the latest version, then exits without opening the window |

---

## Troubleshooting

**A download fails or a site stopped working.** Update yt-dlp first: **Tools → Update yt-dlp**, or run `GoVid --update`. This fixes most problems, because sites change and yt-dlp follows them.

**"Sign in to confirm you're not a bot", or an age-restricted, members-only, or private video.** Set up [sign-in cookies](#sign-in-cookies). If you use **Simultaneous Downloads**, set it back to 1.

**Cookies from Chrome or Edge do not work.** These browsers lock and encrypt their cookies on Windows. Use Firefox, close the browser before downloading, or export a `cookies.txt` file.

**YouTube offers fewer formats than expected.** Make sure a JavaScript runtime is installed. **Tools → Components** installs Deno.

**The video is not available at my chosen quality.** GoVid downloads the closest version and says so in the log. Open **Formats…** to see what the site offers.

**Post-processing is very slow.** Re-encoding is heavy work. Check the **Estimated Processing Load** meter, choose a GPU **Encoder Backend**, and avoid WebM, which always encodes on the CPU.

**GoVid freezes.** Turn on **Debug Output** in Preferences and reproduce the freeze. The heartbeat GoVid then writes to the log every 10 seconds shows how long the window took to answer, how many background tasks and yt-dlp and FFmpeg processes were running, and how many log lines were waiting to be shown.

### Reporting a bug

**Help → Copy diagnostics** puts a report on the clipboard, and **Save as file…** saves it. It contains GoVid's version, your system, the versions of yt-dlp, FFmpeg, and the JavaScript runtime, the GPU check, every setting, the queue, and the last 200 log lines. Your user folder and user name are replaced with `%USERPROFILE%` and `%USERNAME%`, and cookies appear only as their source, never as a path or contents.

Read through the report before you post it anywhere public: it still contains the titles and links of what you downloaded. Then [open an issue](https://github.com/DunderGG/govid/issues) with the report and a description of what happened.

For a full record of a session, turn on **Save output to log file**. GoVid then writes `GoVid_log_YYYY-MM-DD.txt` to the save folder, and copies errors to `GoVid_errors_YYYY-MM-DD.txt`.
