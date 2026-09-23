# Eerraa Windows release contract

The maintained source is `eerraa/agentdock` main, based on upstream 1.1.4.
Development uses feature branches/worktrees. Upstream tags and releases are not
our update channel. Public publication requires separate explicit authorization.

## Version and source identity

The product version is `1.1.4001`: numeric patch `4 * 1000 + 1` represents upstream
patch 4 and downstream revision 1. Use three numeric components; suffixes are not
revisions in the current updater. Future downstream revisions increment the final
component (revision range 1..999). A future baseline change needs an explicit
version-policy review, not automatic upstream synchronization.

Core, WPF and Setup use this same product version. Core `version --json` preserves
legacy 12-character `commit` and adds complete `source_commit`, `distribution`,
`upstream_version`, and `downstream_revision`. WPF embeds the source revision in
its informational version. Build reports record the full commit. Final builds
require a clean source tree; packaging fixes require a new commit and new tests.

## Offline updates and CI

Only independently verified offline Setup.exe packages are supported. `update
--check` returns `code=online-updates-disabled`, `channel=offline-manual`, no known
latest version, and no update offer. Online apply fails before HTTP or mutation.
The desktop translates this stable code using existing UiStrings resources.
Local-archive installation/update remains owned by the existing generation engine.
The installer refuses requests without its offline payload. No upstream fallback.

Main/tag pushes in `windows-package.yml` build candidates for this repository with
read-only contents access. They never publish. The separate publishing job requires
manual dispatch from main, `publish=true`, repository authorization variable
`EERRAA_ENABLE_PUBLIC_RELEASE=true`, exact source/tag verification and the existing
package checks. No signing credentials or publication approval are assumed.

Package creation, complete installed-package acceptance and production replacement
are separate states. CI fixture upgrades do not replace testing the actual previous
production version. An unsigned package must be labeled unsigned; checksums do not
establish a verified publisher. Do not change global trust or bypass UAC/SmartScreen.

## Bundled Windows search component

Windows x64 contains `tools/rg/rg.exe` and the pinned official COPYING/licence
notices in each generation. The authoritative build/runtime pins are in
`internal/bundledrg/windows-amd64.json`; downloads never follow `latest`. The
packager verifies the official ZIP and each extracted file, including licences,
and rejects malformed/incomplete/duplicate payloads. The official binary is not
re-signed as an AgentDock-authored executable.

`search_text` chooses the verified sidecar of the actually running Core first,
then an allowed PATH lookup, then the existing Go fallback. It never consults the
active pointer to choose a different running generation's tool. Completely absent
bundles (legacy/development layouts) permit fallback; present but partial, wrong-
architecture, redirected or changed bundles fail with `BUNDLED_TOOL_INTEGRITY`.
The Windows executable is held open without write/delete sharing until the search
exits. Verification is bounded and cancellation-aware. Result metadata preserves
`engine=rg` and adds `engine_source`, `engine_path`, and bundled `engine_version`.

Structured searches use `--no-config`: an external `RIPGREP_CONFIG_PATH` with
`--invert-match` was reproduced returning nonmatching lines instead of the user's
query. Normal no-match exit 1 remains success; regex/other errors are not converted
to Go fallback. Existing `-e <query> -- <path>` protection remains intact.

Generation publish/upgrade/same-version repair and journal rollback carry the
component with Core/tray. An old flat-layout local-archive updater refuses new
bundled payloads before mutation and directs the user to offline Setup instead of
silently dropping the component. Other OS/architectures keep their existing search
behavior. System/user/ordinary command-session/WSL PATH is not changed; this is not
a promise that typing `rg` in a normal shell will work.

The mandatory real binary suite uses `-tags bundled_rg_integration` with
`AGENTDOCK_TEST_RG_BUNDLE` pointing at the verified component. Missing fixtures fail,
never skip. The package workflow prepares it before these tests. Its PATH fixture
is confined to that disposable CI job for older PATH-specific regressions; tests
of bundle selection explicitly remove PATH. Unit generation/journal fixtures are
not evidence of an installed package; actual final Setup acceptance is separate.

## Windows presentation resources

ActivityText and the execution center use the existing UiStrings ResourceManager
and explicit UiText resource culture. State/action/tool identifiers and user text
remain data. The server's `title_source=fallback` identifies a generated default
conversation title; a user title equal to a historical Chinese label is not a
translation key. Ephemeral warning/detail view codes, not translated captions,
select which asynchronous presentation update may replace an existing message.

Approval dialogs preserve the immutable request and the original approve/reject,
permission scope and revision contracts. Task cancellation guidance distinguishes
changing task state from stopping command processes. The execution filter bar
wraps within an auto-sized row so longer captions do not cover other controls.

`scripts/test/test-windows-activity-center.ps1` includes loaded local-fixture views,
real modal button behavior, user-data preservation, resource/format parity, three
locales/themes, minimum-size control bounds, and 12/14/20-DIP font checks. Captured
100/125/150/200 raster scales are not actual operating-system DPI tests and do not
replace acceptance of the resources installed by the final Setup.exe. Passing
these source fixtures does not establish full product-language coverage.

Generated execution-row tool labels carry `activity_label_source=tool` in the
existing activity event and call projection. This is presentation provenance,
not an execution binding or permission field, and is never accepted as a tool
argument. Windows renders these labels through the existing UiStrings resources
using the canonical tool identifier. Explicit labels, external span labels and
older history without provenance remain verbatim; an identical English label
is not evidence that the user intended an automatically translated title.
