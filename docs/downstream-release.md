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
