# GoVid Refactoring Roadmap — Priority Sorted

This version groups the audit items into priority buckets so you can tackle the biggest maintainability wins first. See the audit for details. [audit_review.md](audit_review.md)

## High Priority

- [ ] Refactor ui.go into smaller helpers — Split the large window construction into helpers for menus, dialogs, history, and preferences so the file is easier to scan and change.
- [ ] Split download.go into phases — Separate yt-dlp argument building, process startup, output parsing, and retry handling into smaller functions. 
- [ ] Break postprocess.go into smaller pipelines — Move FFmpeg option building, UI state handling, and feature-specific logic into smaller functions or separate files.
- [ ] Use context.Context consistently for cancellation — Pass context through the download pipeline so stopping a job does not leave background work running.
- [ ] Group UIWidgets into smaller structs — Break the large UIWidgets type into smaller feature-specific structs like download controls and preferences controls.
- [ ] Keep main.go thin — Use main.go as a bootstrapper only, and move app-specific setup into smaller constructors or services.

## Medium Priority

- [ ] Centralize preference loading — Move preference reads and default values into a small settings-loading layer so UI code stays focused on layout and event wiring.
- [ ] Extract shared window-focus logic — Create one helper for the repeated focus-or-create pattern so every dialog and tool window behaves consistently.
- [ ] Replace hard-coded post-processing thresholds with constants — Name the thresholds and cost values so the code self-documents what each value means and is easier to tune later.
- [ ] Keep LogManager focused on one job — Separate file appending and log persistence from mutex and error-handling details if the type grows further.
- [ ] Move history handling behind a service boundary — Keep storage and schema changes away from the UI so history can evolve without touching the main window code.
- [ ] Keep log parsing tolerant — Treat yt-dlp output parsing as best-effort so small wording changes do not break downloads.

## Low Priority

- [ ] Organize helpers.go by purpose — Split helpers into groups like parsing, filesystem, UI, and formatting so the file does not become a dumping ground.
- [ ] Make helper functions narrowly named and testable — Use descriptive helper names and prefer deterministic helpers for time, byte, and formatting logic so they are easy to test.
- [ ] Keep theme code isolated and reusable — Keep theme colors and helpers separate from UI construction, and use named constants or helpers for repeated colors.
- [ ] Isolate icon and embedded asset code — Keep generated or embedded asset files separate from application logic so they stay predictable and easier to update.
- [ ] Preserve platform-specific wrappers — Keep Windows and non-Windows process handling in dedicated build-tag files so the rest of the app can stay cross-platform and simple.

