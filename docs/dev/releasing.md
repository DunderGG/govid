# Releasing GoVid

A release is a date tag, a zip and a `SHA256SUMS` file built by `package.ps1`, and a GitHub release that holds exactly those two files. GoVid's update check and **Update now** look for exact names, so a release that skips a step can look fine on GitHub but never reach users. Work through the checklist in order.

## What GoVid expects from a release

| Item | Must be | Why |
| :--- | :--- | :--- |
| Tag | A date, `YYYY.MM.DD`. A second release on the same day adds a part: `2026.10.15.1` | The tag becomes the version GoVid reports. `compareVersions` (`release_service.go`) compares the parts as numbers, and a longer version sorts after its prefix |
| Zip | Exactly `GoVid_<tag>_Ready.zip` | `findUpdateAssets` (`self_update.go`) looks for this name. Any other name means **Update now** is never offered |
| Checksum | An asset named `SHA256SUMS` that lists the zip | **Update now** is not offered without it, and refuses a zip whose hash does not match |
| Release state | Published, not a draft or pre-release, and marked as latest | The update check reads GitHub's `/releases/latest`, which skips drafts and pre-releases |
| Release notes | Markdown written for users | GoVid shows them under **What's new** |
| Build | Built by `package.ps1` | Only it sets `main.buildType=release`. Builds from `build.bat` or `build.sh` never update themselves |

Installed copies check for a new release at most once a day. **Tools → Check for GoVid updates** checks right away.

## Checklist

### 1. Prepare

- [ ] `main` is clean and pushed, and CI passed on Windows and Ubuntu for the commit you will tag.
- [ ] The documentation matches the release: [README.md](../../README.md), [user-guide.md](../user-guide.md), and the in-app guide (`helpItems` in `help_window.go`).
- [ ] `external/` holds current tools:
  - `yt-dlp.exe` from the [latest yt-dlp release](https://github.com/yt-dlp/yt-dlp/releases/latest). An outdated yt-dlp is the most common reason downloads fail, so a release should not ship an old one.
  - `ffmpeg.exe` from the release essentials build on [gyan.dev](https://www.gyan.dev/ffmpeg/builds/).
- [ ] Smoke test a normal build: run `.\build.bat` and `.\GoVid.exe`, download one video, and download one with a post-processing filter on.

### 2. Tag and package

- [ ] Tag the commit, for example `git tag 2026.10.15`.
- [ ] Build the package:

  ```powershell
  powershell -ExecutionPolicy Bypass -File .\package.ps1
  ```

  It stops if the commit has no tag or `external/` is missing a tool. It writes `GoVid_<tag>_Ready.zip` and `SHA256SUMS` to the repository folder.
- [ ] Check the output:
  - The zip name contains the exact tag.
  - In Git Bash, `sha256sum -c SHA256SUMS` prints `OK`.
  - Extract the zip to a new folder outside the repository and run `GoVid.exe`. **Help → About GoVid** shows the tag as the version, and `VERSIONS.txt` names the right commit and tool versions.

### 3. Publish

- [ ] Push the commit and the tag:

  ```bash
  git push origin main
  git push origin 2026.10.15
  ```

- [ ] [Draft a new release](https://github.com/DunderGG/govid/releases/new) for the tag:
  - Title: `GoVid <tag>`.
  - Notes: what changed, for users. They appear in the app under **What's new**.
  - Assets: upload exactly `GoVid_<tag>_Ready.zip` and `SHA256SUMS`. Do not attach a separate `GoVid.exe`: it has no `bin` folder, and it leaves visitors guessing which file to download.
  - Leave **Set as a pre-release** unticked and **Set as the latest release** ticked, then publish.

### 4. Verify

- [ ] <https://api.github.com/repos/DunderGG/govid/releases/latest> shows the new `tag_name` and both assets.
- [ ] Test **Update now** with an older release build. Build one in a scratch folder, so it is not overwritten by the build you shipped:

  ```powershell
  go build -ldflags="-H windowsgui -X main.version=2000.01.01 -X main.buildType=release" -o C:\Temp\govid-update-test\GoVid.exe .
  ```

  Run it, choose **Tools → Check for GoVid updates**, and use **Update now** from **What's new**. GoVid should restart, and **Help → About GoVid** should show the new tag.

## If something goes wrong

- **Wrong zip name or missing `SHA256SUMS`:** edit the release and replace the assets. Installed copies read the asset list each time they check, so the fix applies at their next check.
- **A broken release:** publish a fixed one under a new tag, such as `2026.10.15.1`. Do not move or reuse a published tag. Installed copies report that tag as their version, so it must always mean the same build.

## Releases before 2026-10

Releases `2026.04.11` and `2026.09.17` predate the update check, so their users are never told about a newer release in the app. Adding `SHA256SUMS` to them would change nothing. The next release is the first one that can update itself. Its users have to download it by hand once, so announce it wherever they will see it.
