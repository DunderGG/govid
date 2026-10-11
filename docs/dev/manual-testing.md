# Manual Testing

Videos to try GoVid against by hand, chosen per feature, and how to test without getting throttled by YouTube.

The automated tests never touch the network. They run a fake yt-dlp (`fake_tool_test.go`) against fixtures in `testdata/`. Manual testing is for what the fake cannot show: real sites, real formats, and how the app looks and feels while it works.

## Accounts and cookies

> **Never test with cookies from your main Google account.** yt-dlp's documentation warns that an account used with yt-dlp can be banned, for a while or for good, and testing sends the bursts of requests that get noticed. Create a separate Google account for testing, without payment details or anything you would miss, and sign in to YouTube with it only for GoVid.

Most tests need no cookies. They are for videos that need you to be signed in (age-restricted, members-only, private) and for getting past "Sign in to confirm you're not a bot". Cookies are a login: anyone with them is signed in as that account. Keep cookie files outside the repository and never share them. (`*.txt` is in `.gitignore`, but backups and copies of the folder still take a file along.)

GoVid reads cookies in two ways (**Tools → Preferences → Cookies**). With a test account, a browser profile is the easier one.

### From a browser profile (recommended)

A separate Firefox profile keeps the test account apart from your everyday browsing, and GoVid reads its cookies fresh at every download, so they never go stale.

1. In Firefox, open `about:profiles` and click **Create a New Profile**. Name it, for example, `govid-test`.
2. Click **Launch profile in new browser** beside it, and sign in to YouTube with the test account in that window.
3. In GoVid, set **Tools → Preferences → Cookies** to **From browser**, the browser to **Firefox**, and the profile to `govid-test`.

Use Firefox rather than Chrome or Edge: on Windows, Chromium browsers lock their cookies while they are open and encrypt them in a way yt-dlp cannot read.

### From a cookies.txt file

A `cookies.txt` file in Netscape format, exported with a browser extension. yt-dlp's FAQ suggests [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/) for Firefox and [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc) for Chrome. A cookie extension can read every cookie in the browser, and malicious ones have been published under similar names, so install only these, and remove the extension when you are done.

YouTube replaces ("rotates") the cookies of a signed-in session while a YouTube tab is open, which makes an exported file stop working within minutes. yt-dlp's wiki gives a way around it:

1. Open a private window (Firefox: **Ctrl+Shift+P**; Chrome: **Ctrl+Shift+N**), and allow the extension in private windows.
2. Sign in to YouTube with the test account.
3. In the same tab, open `https://www.youtube.com/robots.txt`. Keep it the only tab in the private window.
4. Export the cookies for `youtube.com` with the extension, and save the file outside the repository.
5. Close the private window. Never open that session in a browser again, or its cookies rotate.

Then set **Tools → Preferences → Cookies** to **From file** and choose the file. When it stops working (the log says the site asks you to sign in), export a new one the same way.

## Ground rules

YouTube rarely bans anyone for testing, but it does throttle an IP address that sends many requests in a short time. You then see "Sign in to confirm you're not a bot", HTTP 429 (Too Many Requests), or refused download links (HTTP 403). GoVid handles all three, but testing anything else is slow until it clears, usually within a few hours.

GoVid sends more requests than it looks like: each URL is probed before it downloads, and each subtitle language is a request of its own. To keep the count down:

- **Test on archive.org or a direct file first.** Use YouTube only for what only YouTube has: separate video and audio streams that are merged, the format browser's long list, subtitles, HDR, and live streams.
- **Use short videos** and set **Max Quality** to 360p unless you are testing quality itself.
- **Keep Simultaneous Downloads at 1**, except when testing it, and then on the short videos below.
- **Do not download the same video over and over.** For duplicate detection, one repeat is enough.
- **Limit subtitle languages** to one or two (`en`, not `all`).
- If you get throttled, stop testing YouTube for a while rather than retrying. Cookies from the test account get past the bot check (see [Accounts and cookies](#accounts-and-cookies)), but they tie the requests to that account, so do not lean on them to keep testing at the same pace.

## Test videos

All links were checked with yt-dlp on 2026-10-11. Links break: a video is taken down, or a live stream restarts under a new ID. See [Checking a link](#checking-a-link) to test one before relying on it.

### Without YouTube

| Use | URL | What it is |
| :--- | :--- | :--- |
| Quickest smoke test | `https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4` | A direct link to a 10-second, 1 MB MP4. yt-dlp's generic extractor downloads it. The same site has larger sizes (`_2MB`, `_5MB`, …) and other codecs. |
| Direct link, larger | `https://archive.org/download/BigBuckBunny_124/Content/big_buck_bunny_720p_surround.mp4` | A direct link to a 720p MP4 on archive.org |
| Single video | `https://archive.org/details/BigBuckBunny_124` | *Big Buck Bunny*, about 10 minutes. Formats: 300p OGV (45 MiB), 360p MP4 (59 MiB), 720p AVI (317 MiB). Good for pause and resume with **Max Download Speed** set to `1M`. |
| Item with two videos | `https://archive.org/details/Sintel` | *Sintel*, in stereo and in 5.1 surround. yt-dlp treats the item as a playlist of two, which tests the playlist prompt without YouTube. 74 MiB and up per video. |

archive.org serves each video as one file with audio, so it never exercises merging separate video and audio streams. yt-dlp also reports its codecs as "unknown", which shows how the format browser copes with missing details.

Blender's own download server (download.blender.org) is behind a Cloudflare bot check that yt-dlp's generic extractor does not get past, so it is not listed.

### YouTube

| Use | URL | What it is |
| :--- | :--- | :--- |
| Shortest download | `https://www.youtube.com/watch?v=jNQXAC9IVRw` | *Me at the zoo*, 19 seconds: the first YouTube video. YouTube serves video and audio as separate streams, so this usually tests merging too, cheaply. |
| Formats, quality, merging | `https://www.youtube.com/watch?v=UXqq0ZvbOnk` | *CHARGE*, a Blender open movie, 4 min 23 s. A long format list for the format browser and **Preferred Video Codec**. |
| Subtitles | `https://www.youtube.com/watch?v=_cMxraX_5RE` | *Sprite Fright*, a Blender open movie, 10 min 30 s. Subtitles written by people in English, German, Spanish, Italian, Russian, Hungarian, Portuguese (Portugal), Bangla, and Malayalam. |
| Auto-generated captions only | `https://www.youtube.com/watch?v=UXqq0ZvbOnk` | *CHARGE* again: no subtitles written by people, only automatic captions. With **Include auto-generated** off, the log warns that no subtitles match; with it on, the captions download. |
| HDR | `https://www.youtube.com/watch?v=LXb3EKWsInQ` | *COSTA RICA IN 4K 60fps HDR*, 5 min 14 s, HDR10. Tests the format browser's HDR column and the **HDR to SDR** filter. Set **Max Quality** to 1080p or lower: the 4K version is large. |
| Short playlist | `https://www.youtube.com/playlist?list=PL0BAFA2DA294B0900` | "Mango Open Movie" (the *Tears of Steel* production diary), 10 videos. The first three are 7, 29, and 65 seconds long: enter the range `1-3` for a quick queue. |
| Larger playlist | `https://www.youtube.com/playlist?list=PLav47HAVZMjnTFVZL-aImCQIC0uLZtNCz` | "Blender Open Movies", 18 videos of 1 to 15 minutes. Use a range: `9,11,14` picks three of the shortest (61, 151, and 147 seconds). Entry 9 is in 8K, which tests **Max Quality** limits. |
| Live stream | `https://www.youtube.com/@LofiGirl/live` | Lofi Girl's 24/7 stream. The `/live` link always leads to the channel's current stream, so it survives the stream restarting under a new ID. Record for a few seconds, then **Stop recording**. |

For a scheduled stream or premiere (**Wait and record**), look for an upcoming premiere on any large channel shortly before testing; there is no stable link for one.

## What to test with what

| Feature | Videos |
| :--- | :--- |
| A first download, file names, history | The 1 MB direct link, then *Me at the zoo* |
| Duplicate detection | Download *Me at the zoo* twice. Also try it as `https://youtu.be/jNQXAC9IVRw`, which GoVid should recognise as the same video. |
| Batch Mode and Load from file | A list of the two direct links and *Me at the zoo* |
| Playlist prompt and ranges | "Mango Open Movie" with `1-3`; the archive.org *Sintel* item |
| Queue tab: move, remove, skip, retry | "Mango Open Movie" with `1-3`, or the batch list above |
| Simultaneous Downloads | "Mango Open Movie" with `1-3` and Simultaneous Downloads at 2 |
| Pause, resume, and restarting GoVid mid-download | archive.org *Big Buck Bunny* with **Max Download Speed** `1M` |
| Cancel | Any download, cancelled partway: its partial files should be deleted |
| Auto-retry | Only network and rate-limit errors are retried (`transientErrPatterns` in `logscanner.go`). Turn the network off partway through archive.org *Big Buck Bunny* and wait until yt-dlp gives up with a connection error, then turn it back on before GoVid's retry starts. |
| Trim Start and Trim End | *CHARGE*, for example `00:00:10` to `00:00:30` |
| Format browser, Preferred Video Codec | *CHARGE*; *COSTA RICA* for HDR formats |
| Subtitles (Embed, Save as .srt, Both) | *Sprite Fright* with languages `en,de` |
| Post-processing filters | The 1 MB direct link (fast); *COSTA RICA* at 1080p for HDR to SDR |
| Live recording | Lofi Girl's `/live` link |
| Disk space check | Set the save folder to a small USB stick or a nearly full drive, then download archive.org *Big Buck Bunny* at 720p |

## Checking a link

To see whether a link still works, and what yt-dlp finds there, without downloading:

```sh
external/yt-dlp.exe --skip-download --print "%(title)s | %(duration)s s | %(live_status)s | %(dynamic_range)s" <URL>
```

Add `--list-subs` to list subtitles (those written by people come first, under "Available subtitles"), `-F` to list formats, or `--flat-playlist` for a playlist's entries. Each check is a request to the site, so on YouTube check only the links you are about to use.
