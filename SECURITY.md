# Security policy

## Reporting a vulnerability

Use [GitHub private security advisories](https://github.com/aignerdevelopment/harnessator/security/advisories/new)
on the repository. No public issue needed, no bounty, credit on request.
Expect an acknowledgement within a few days; this is an Apache-2.0
project maintained by one engineer and users like you - not a vendor
with a security team, and the fix cadence follows the release cadence
(patch releases as needed).

## Supported versions

The latest release only (any tag >= current minus a few). `harnessator
selfupdate` and the checksummed installers keep you on it; there is no
long-tail version support in beta-successor land. If you are pinned
older, say so in the report - the repro may differ.

## What the tool's security model actually is

Honest, in three lists. This mirrors [the security review
guide](docs/guides/platform/security-review.md) - it argues both sides.

**What harnessator protects against:**

- *Supply chain of agent config*: every component is pinned to an exact
  commit (and content commit for pointer sources), digests are checked
  offline, drifted or aliased store clones are refused, anchored tags
  never move. `harnessator check` is the auditable verdict; `--json` is
  a stable contract for your pipeline.
- *Secret leakage by design*: configuration layers transport env-var
  NAMES only. Literal keys in scanned configs are reported as findings,
  never copied; credential helper output and file contents are never
  rendered - not in diffs, labels or errors (test-enforced).
- *Privileged file writes*: the machinery that writes outside its own
  state (harness configs via `provider modelupdate`) runs with backups,
  confined text splicing (byte-identical outside the edited region), a
  verify-or-fallback safety net, mass-removal and current-model guards -
  fuzz targets and `-race` gates keep it provable, not plausible.
- *Destructive surprises*: refusal-first culture. Anything that would
  delete at scale or move a shared name stops, explains, and prints the
  paste-ready ramp.

**What it does NOT protect against (and never claims to):**

- It does not sandbox your agents, scan prompts, or police what a
  harness or model does at runtime. Governance is about WHAT is
  configured and WHO approved it - not about containing a compromised
  agent.
- It does not manage secrets. Values stay in your env, your auth store,
  your vault - which also means it cannot protect you from a secret you
  paste into a source repository yourself.
- It is not a certificate authority: binaries ship sha256-verified over
  HTTPS from GitHub releases. Artifact signing (cosign/minisign) is a
  documented pending decision - see [Distribution.md](docs/wiki/Distribution.md)
  - so trust today is integrity + pinned tags + your own eyes.

**Attack surface you should keep an eye on:**

- Source repositories are as trusted as their maintainers: adding a
  source means running its rules/skills inside your harnesses. Review
  them like a dependency; `provider import`-only/`--require-committed`
  modes exist for a reason.
- `dev` mode and `contribute` touch real remotes by design; they are
  governed git, not a safety boundary against a hostile upstream.

## Notes for decision makers

The v1.0 public surface is a written contract
([Contract-v1.md](docs/wiki/Contract-v1.md)) enforced by golden tests;
breaking changes require deliberate, announced amendment. The full
CHANGELOG is an engineering diary, including the failures - a security
posture you can read is a security posture you can verify.
