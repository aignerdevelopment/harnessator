# Development Diary & Changelog

harnessator's design history, distilled. The public repository starts as
a single consolidated commit **by design** - the messy argumentation of
development is squashed into this diary so every reader gets the *why*
without needing the *thread*. Newest first.

Attribution, honestly: this tool was built pair-style - Roland Aigner
driving, an AI agent (in OpenCode) doing much of the typing and the
record-keeping. That partnership is documented here, in this diary;
the public commits deliberately carry one human author - no bot
co-author trailers.

## v0.1.0-beta (in preparation)

**Scope layers became physical.** Precedence is no longer a label on one
file: five optional files - corporate (`/etc/harnessator/config.toml`),
team (`harnessator.team.toml`), project (`harnessator.toml`), personal
(`~/.harnessator/personal.toml`) and local (`.harnessator/local.toml`,
per-project, gitignored) - merge into one effective config. Additive
semantics: sources union with a hard conflict on same-name/different-URL,
templates and selects concatenate in ascending precedence, the highest
layer naming a provider wins, any strict corporate lock wins. `enable`,
`disable` and `source add` route their writes by `--layer`; `harnessator
layers` makes the merge visible; provenance names the introducing file
(`[via team]`) - which is what makes "embedded-me" and "ux-me" share the
corporate sources per task without crosstalk.

**Local-first registry.** `harnessator index build|search|show` walks the
store's pinned sources into one rebuildable JSON index
(`~/.harnessator/cache/index.json`) with full provenance per component
(git, ref, commit). Search and show run without a project and without
network. The registry stays a *format*, not a service: no server is ever
required, and a corporate hub, if one is wanted, is just a cron job over
the same index builder.

**MCP server for agents.** `harnessator mcp serve` speaks JSON-RPC 2.0
over stdio (newline-delimited, protocol `2024-11-05`) with zero added
dependencies. Five read-only tools - `search`, `list_active`, `why`,
`component_content`, `policy_status` - let an agent inside a configured
project introspect its own harness configuration; `component_content`
reads the exact pinned checkout the lock digests cover. Mutations stay
deliberately CLI-only. The server ships as an `mcp` component in the
example source, so `setup` wires it into Claude Code, opencode and Codex
natively.

**Policy-enforcement gate.** `harnessator check` is the automation
surface: offline (lock pins only, never fetches), one exit code,
`--json` for pipelines. It validates strict model policy, lock/digest
integrity and installed-state drift in one command.
`--install-hook pre-commit|pre-push` writes a managed, self-skipping
block into `.git/hooks/` - it refuses to clobber or delete foreign
hooks, because honesty extends to a project's existing tooling.

**Documentation as a gate, not a promise.** Every command verb and every
flag is machine-checked against `USAGE.md` and the man pages
(`internal/cli/docs_test.go`); the pages must render under groff; the
pitch must keep up with the code. Docs drift is a build failure.

## 2026-10-19 — governance: styles and dialects

Provider profiles went through one deliberate breaking redesign *before
anyone else used them* (migration cost is zero exactly once): endpoints
are keyed by **API style** - `openai-compatible`,
`anthropic-compatible`, `openai-responses` - not by harness product. A
LiteLLM-style gateway exposing several protocol families at different
paths is data, not code. Rendering moved into one **dialect** per
governance-capable target (opencode, claude, codex): dialects declare a
style priority and emit pure-data fragments
(`Result{JSON, Env, TOML}`); harness conventions (npm packages by style,
Anthropic tier-alias env vars, codex `wire_api`) are tables inside the
dialects. Capability = registration: targets without a dialect, or a
profile without a speakable style, report a loud skip - never a silent
drop.

Decisions taken and logged here so they are not relitigated: Go `.so`
plugins rejected (they would break the static, cross-compiled,
single-binary distribution and the git-review + digest supply chain);
out-of-process adapters deferred, not rejected - the **extraction
criterion** is the first third-party adapter that needs releases
out-of-band with the core, and every interface crossing the adapter seam
is pure data so that extraction stays a transport wrapper. A hosted
curated artifact store is explicitly *parked*: the registry is the
format; custody services can come much later.

## 2026-10-19 — harvest: publishing what you already have

The cold-start problem: teams adopted AI harnesses before any tool
existed, so skills, rules and prompts are scattered across project trees
- duplicated, diverged, half-secret. `harnessator scan` is the read-only
half: it classifies components by capability recognition (layout
knowledge lives next to each adapter's *writer*, so reading and writing
can never drift apart), groups copies by content digest, flags
cross-project divergence and masked secret/path findings, and emits a
versioned JSON envelope built for dashboards.

`harnessator harvest` is the write half: it turns candidates into a
validated, git-ready source repository. Inputs are never modified;
every conflict, sanitize finding and invalid id aborts **before any
write**; identical copies merge; divergent copies need an explicit
`--prefer newest|largest`; re-runs are idempotent; the written manifest
self-validates. Config-embedded content (MCP server definitions in
harness configs, hook scripts referenced from Claude settings) is
lifted canonically; what cannot be classified lands in a machine-readable
**residual report** for the guided tail - a skill (`harnessator-harvest`)
plus an agent that classify the leftovers, while the tool ratifies:
nothing becomes real until the deterministic validation passes. Agent
drafts, tool ratifies - that is the whole security model of the loop.

`--adopt` turns every donor project into a consumer of the published
source: managed links replace the scattered originals, a original is
only ever deleted after its content matches the published digest;
divergent originals stay (and `setup` loudly skips the blocked
destination), generated-path collisions go to `*.pre-adopt` backups.

## 2026-10-18 — the core: git as the only server

Architecture decided up front and still holding: a single static Go
binary, stdlib-first (toml + x/mod + x/sys are the only dependencies),
cross-compiling for linux/mac/windows; the **system git** does the
gitting (native auth, credential helpers, worktrees - go-git rejected);
a per-user store (`~/.harnessator`) of pinned, read-only checkouts;
projects keep only committed `harnessator.toml` + `harnessator.lock`;
`harnessator.lock` pins exact commits *and* per-component sha256 digests
so that post-approval changes in a source are detectable in the diff of
a lockfile bump. Scopes layer corporate → team → project → personal;
profiles and templates make bulk selection reviewable; `why` answers
provenance for any enablement or skip.

Adapters are honest by construction: a component type a harness has no
native slot for is reported as a `SKIP`, generated files are written
whole or spliced between unmistakable managed markers, human-owned
entries in shared files are never touched, and drift in any direction
fails `verify` (and, in CI, the build). Windows gets NTFS junctions
instead of symlinks (no admin/developer mode) plus digest-verified
copies for single files - same guarantees, native mechanism.

Governance landed as the differentiator: provider profiles (endpoints +
approved model catalog + aliases) and policy profiles (strict by default,
overridable only by an explicit higher layer). Enforcement happens
inside the tool, not in a dashboard: the opencode model picker is
narrowed to the allowed set, Claude Code gets the endpoint via env with
**sub-agent model pinning** (`CLAUDE_CODE_SUBAGENT_MODEL` - the
"sub-agents ignore the configured model" collision, solved), Codex gets
managed `[model_providers]` stanzas. Secret values never touch git, the
store or any generated artifact - only env var *names* do.

Development has always dogfooded: this very repository is managed by
harnessator - committed lock, generated harness directories, and (since
the beta tooling) `harnessator check --json` returns `{"ok":true}` as a
real gate on every clone.

## 2026-10-17 — origin: configure harnesses like infrastructure

The starting observation: every AI coding harness (Claude Code, Codex,
opencode, Cursor, pi, Antigravity, ...) wants its own config files in
its own folders, teams duplicate rules by copy-paste, the approved model
list lives in a wiki nobody reads, and none of it is reviewable or
reproducible. The shape that fixes this is the one infrastructure
already trusts: declarative config in git, dependency-style pinning,
plan/apply, drift detection, merge-request review. Hence the standing
pitch - **Terraform for AI coding agents** - while being clearer about
what is *not* copied: no cloud API, no provider plugins, no state
backend. Git is the only server.

## Stability policy at beta

- **Frozen (breaking = deliberate decision):** the source format -
  manifest schema, `(git, ref, manifest)` identity, lockfile semantics
  with commit pins + sha256 component digests, canonical API styles,
  reference grammar. This is what third-party sources and future tooling
  build against.
- **Stable-ish, may evolve:** adapter layouts per target (driven by what
  each harness actually consumes), the web/TUI surfaces, dev/contribute
  convenience flows.
- **Planned:** expansion adapters (gemini, cline, copilot, windsurf -
  each one dialect + layout data now), full-screen TUI, gallery view,
  the project homepage.

Want a component source published, an adapter for your harness, or a
policy story that does not fit? Open an issue - loud opinions welcome.
