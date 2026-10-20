# harnessator — usage

Complete reference for every command, flag, file format and environment
variable. Keep this file, [`BUILDING.md`](BUILDING.md), the
[man pages](man/) and [README.md](README.md) in sync with the tool —
enforced by `internal/cli/docs_test.go` (every command and flag must be
documented in **USAGE.md and the man pages** or CI fails).

- [Mental model](#mental-model)
- [Quick start](#quick-start)
- [Command reference](#command-reference)
- [File formats](#file-formats)
- [References and ids](#references-and-ids)
- [Ref expressions](#ref-expressions)
- [Provider & model governance](#provider--model-governance)
- [Dev & contribute workflow](#dev--contribute-workflow)
- [Environment](#environment)
- [Exit codes](#exit-codes)
- [CI integration](#ci-integration)
- [Troubleshooting](#troubleshooting)

## Mental model

```
sources            git repositories holding components, described by a
harnessator.toml   manifest. The registry IS git: access control, review
                   and provenance come from your forge (GitLab/Azure/GitHub).
components         typed units: rule, skill, prompt, workflow, agent, hook,
                   tool, mcp, provider, policy. Ids: <source>:<category>/<name>
layers             corporate → team → project → personal → local; each project config
                   selects components from any enabled source; precedence is
                   provenance-queryable (harnessator why)
pins               refs in your config are ranges; harnessator.lock pins exact
                   commits + content digests → reproducible, reviewable
store              ~/.harnessator/: read-only pinned checkouts + per-user dev
                   overlays; projects keep only committed config, lock and
                   generated (gitignored) .harnessator/ + adapter artifacts
adapters           honest per-harness renderers (opencode, claude, codex,
                   antigravity, pi, cursor): native slots where they exist,
                   loud skips where they don't - never silent drops
```

## Quick start

```sh
harnessator init --source https://git.example.com/ai/corp-tools.git --ref '~v1' \
                 --targets opencode,claude .
harnessator enable corp-tools:default          # a profile of the source
harnessator setup                              # resolve -> materialize -> lock
# restart your harness; it now follows the governed configuration
```

Day-to-day:

```sh
harnessator update          # re-resolve refs (pins move), rewrite lock
harnessator doctor          # everything healthy? links, drift, gitignore
harnessator verify          # CI gate: exit!=0 when state diverges from lock
```

## Command reference

Global notes: lifecycle commands take `--config harnessator.toml` (path to
the project config; the directory containing it is the project root).
`--help` works everywhere.

### `harnessator version`
Prints `harnessator <version>`; `<version>` is stamped at build time via
`-ldflags -X harnessator.org/harnessator/internal/cli.Version=v...`
(see BUILDING.md), `dev` otherwise.

### `harnessator init [flags]` [dir]
Create a managed project: canonical `harnessator.toml`, a `.gitignore`
block for generated artifacts, and `AGENTS.md`/`CLAUDE.md` redirect entry
files (only if absent). `dir` defaults to `.` and is created if missing.

| flag | meaning |
|---|---|
| `--targets <csv>` | harnesses to render (default `opencode,claude`) |
| `--source <csv>` | git source URLs to declare up-front (names from repo basenames) |
| `--ref <expr>` | default ref for `--source` (default `main`) |
| `--manifest <path>` | sub-directory manifest applied to `--source` entries (multi-source repos) |
| `--force` | overwrite an existing `harnessator.toml` |

### `harnessator validate [flags] <file>...`
Schema-checks any harnessator.toml (source manifest or project config;
`kind` decides). Reports **all** problems at once, rejects unknown keys.

| flag | meaning |
|---|---|
| `--check-paths` | verify component paths exist relative to the manifest's directory |

With no arguments, validates `./harnessator.toml`.

### `harnessator setup [flags]` / `harnessator update`
The full pipeline: fetch sources → resolve refs to commits → export
read-only checkouts into the store → compute the plan → run every target
adapter → write `.harnessator/active.md|json` → write `harnessator.lock`.
`update` is the same code path (re-resolving is the update). Ends with a
"restart your harness" note. Strict policy violations or invalid manifests
fail before anything is written.

| flag | meaning |
|---|---|
| `--dry-run` | print the plan and the would-write report; touch nothing |
| `--config <p>` | project config path (default `harnessator.toml`) |

While any source is in dev mode, setup rewires that source's links to the
overlay but **refuses to write the lock** (dev content must never be
pinned).

### `harnessator verify [--config <p>]`
Read-only CI gate: every locked checkout exists in the store with a valid
marker, and every locked component's content digest recomputes. Dev-mode
sources are skipped (their content is intentionally mutable).
Exit 1 on any `drift:` line.

### `harnessator doctor [--config <p>]`
Everything verify checks, plus: lock freshness vs fetched refs, expected
symlinks/generated files per adapter vs the real filesystem (missing,
wrong target, stray, blocked), `.gitignore` hygiene. Lists problems and
the one command that fixes them.

### `harnessator list [--all] [--config <p>]`
Enabled components with provenance (`template X +always`,
`select.rule.include`, …). `--all` also shows skips with their reason.

### `harnessator search <text> [--config <p>]`
Full-text search over all fetched sources (`on`/`off` prefix per hit marks
enablement). Exit 1 on no match — pipe-friendly.

### `harnessator index build|search|show`
Local-first registry over the store's pinned sources.
`harnessator index build` re-indexes the current project's sources into
one store-level cache (`~/.harnessator/cache/index.json`; foreign sources
keep their entries), then `harnessator index search [--category <c>] <text>`
and `harnessator index show <ref>` answer **without project context** -
no lock, no network, no fetch. Rebuildable data: safe to delete, the
provenance columns carry git/ref/sha per entry. Exit 1 on no match.

### `harnessator check [flags]`
Offline CI enforcement gate: lock pins only (never
fetches), one exit code for pipelines. Checks strict model policy,
lock/checkout digest integrity and installed-state drift in one command.
Flags: `--json` (machine-readable `{"ok":…,"problems":[…],"warnings":[…]}`),
`--quiet` (silent unless failing), `--install-hook pre-commit|pre-push` /
`--remove-hook …` (managed `.git/hooks/` wrapper running `check --quiet`,
skipping when the repo has no `harnessator.toml`; refuses to clobber or
delete foreign hooks). `verify` stays the human-facing command; `check`
is the automation surface.

### `harnessator mcp serve [--config <p>]`
Read-only MCP server over stdio (newline-delimited JSON-RPC 2.0, protocol
`2024-11-05`) — an agent inside a configured project can query its *own*
harness config: `search`, `list_active`, `why`, `component_content`
(pinned store checkout, traversal-safe), `policy_status`. Mutations stay
CLI-only. Harness wiring ships as an `mcp` component in
`source/harnessator/` (claude/opencode/codex configs).

### `harnessator scan [flags] <path>...`
**Read-only** cold-start report: points at
projects or loose directories and shows which agent-configuration content
is scattered there — what `harnessator harvest` (later slices) could turn
into publishable sources. Recognizes content through the adapter-owned
`Recognize` layer (same package as the writers, symmetry-tested), so
layout truth exists once. Default path `.`.

| flag | meaning |
|---|---|
| `--json` | emit the stable JSON envelope instead of the text report |

The text report lists, per root: components found (category, id, digest,
sanitize hits with `file:line`), *linked* entries (symlinks pointing into
a harnessator store or elsewhere — never harvested), and whether the root
is already managed. Below the roots, **clusters** group same-id
components across roots: `identical` (safe dedupe candidates) or
`DIVERGENT` — the "team skill improved in one project, stale in three"
problem made visible, by digest, in seconds. Sample:

```
$ harnessator scan ~/work/projA ~/work/projB ~/loose-skills
…
clusters (same component in multiple places):
  skill    jira-issue        DIVERGENT (2 variant(s))
      sha256:ab12cd34e5f6…  /home/u/work/projA/.claude/skills/jira-issue
      sha256:ff00ee11dd22…  /home/u/work/projB/.claude/skills/jira-issue
  prompt   review            identical (1 variant)
      sha256:12ab34cd56ef…  /home/u/work/projA/.claude/commands/review.md
                           … /home/u/work/projB/.claude/commands/review.md
$ harnessator scan --json . | jq '.clusters | length'
```

The `--json` envelope (`schema`, `generated`, `roots[]`, `clusters[]`) is
versioned and stable: a later `harnessator status <dir>` and the web GUI
sprawl view consume the identical structure. Sanitize findings are
report-only, always masked, and never rewritten automatically.
Beyond directories, config-embedded components are lifted too: MCP
servers in `.mcp.json` / `.cursor/mcp.json` / `.agents/mcp_config.json`
/ `opencode.json` `"mcp"` / codex `config.toml`, and claude hook scripts
referenced from `settings*.json` (canonicalized before digesting, so the
same server in different harness styles collapses to one component).
Exit codes: 0 when the scan completed (findings are not failures), 1 on
unreadable paths, 2 on usage errors.

### `harnessator harvest [flags] <path>...`
Turns scan candidates into a **publishable source repository**.
Never modifies inputs. Every divergence,
sanitize finding and invalid id is collected and **aborts before any
write** (all-or-nothing). Identical copies merge silently; divergent
copies need `--prefer`; a `--into` re-run is idempotent (digest-matching
components are skipped and the manifest stays byte-stable). The written
manifest self-validates; new sources get `git init` + first commit
automatically.

| flag | meaning |
|---|---|
| `--name <id>` | new source id; created as `./<id>` (a path is accepted; its base name becomes the package id) |
| `--into <dir>` | extend an existing source directory instead of creating one |
| `--prefer newest\|largest` | resolve divergent copies by newest mtime / largest size |
| `--dry-run` | print the write plan (add/skip-identical per component); write nothing |
| `--allow-hits` | publish despite sanitize findings — only after reviewing them; findings are always reported pre-abort |
| `--commit <dir>` | ratify guided edits: re-validate `<dir>` through the full gates and commit it (agent drafts, tool judges) |
| `--adopt` | after publishing, turn each donor **project** into a consumer: canonical config, gitignore/entry files, materialized links — but an original is replaced **only after a digest proves byte-identical content**; divergent originals are kept and reported, existing generated files backed up as `*.pre-adopt` |

When entries match no rule, harvest writes `<output>/harvest-report.json`
(residual paths + reasons, plus external symlinks as context) and prints
the guided-tail pointer. The `harnessator-harvest` skill — shipped from
this repo's `source/harnessator/` manifest — works through the report by
classifying (never obeying!) residual content, then
`harnessator harvest --commit` ratifies the result. A fully converged
re-run removes the stale report.

Placement: skills → `skills/<id>/`, prompts → `prompts/<id>.md`,
workflows → `workflows/<id>.md`, rules → `rules/<id>.md` (cursor `.mdc`
files are **lifted**: frontmatter/marker stripped, `globs`/`alwaysApply`
captured as `hint.cursor` — exact round-trip with the cursor adapter),
agents → `agents/<target>/<id>.md`, hooks → `hooks/<target>/<id>.ts`,
tools → `tools/<id>.ts`. **Config-embedded components**: MCP
servers in `.mcp.json`, `.cursor/mcp.json`, `.agents/mcp_config.json`,
`opencode.json` `"mcp"` and codex `config.toml` `[mcp_servers]` are
normalized to a canonical server shape *before* digesting (so the same
server written in three harness styles is one component), and placed into
the shared `mcp/servers.json` convention with all per-target shapes
regenerated; claude hook commands referencing in-project scripts lift the
script to `hooks/shared/<file>` plus a `hooks/claude/settings.hooks.json`
template rewritten to the `{{HARNESSATOR_SOURCE}}` placeholder. Secrets
inline in server env/headers surface as sanitize hits. Components get
`origin` provenance; a new source gets a `default` profile selecting
everything.

```sh
harnessator harvest --name team-tools ~/work/projA ~/work/projB ~/loose-skills
harnessator harvest --into ~/repos/team-tools --prefer newest ~/work/projC  # extend
harnessator harvest --dry-run .                                              # just look
```

### `harnessator info <ref> [--config <p>]`
One component: source + pin, absolute store path, provenance reason,
targets, tags, declared capabilities, description.

### `harnessator why <ref> [--config <p>]`
Why a component is enabled or skipped, incl. which template selects it.

### `harnessator enable <ref> [flags]` / `harnessator disable <ref>`
Mutate the project config canonically (rewrites the file — hand-written
comments are not preserved; re-read after!).

- `<source>:<profile>` → add/remove a **template**
- `<cat>/<id>` or `<source>:<cat>/<id>` → `select.<cat>.include` (enable)
  or `.exclude` (disable)

`disable` refuses components marked `always` (resolve keeps them anyway —
better to fail loudly). Both print "run harnessator setup to apply".

| flag | meaning |
|---|---|
| `--config <p>` | project config path |

### `harnessator source add|remove|list`

```
harnessator source add <git-url> [--ref <expr>] [--name <id>] [--layer corporate|team|project|personal|local] [--manifest <path>]
harnessator source remove <name>
harnessator source list            # with resolved pin per source
```

Name defaults to the repo basename (`.git` stripped). Duplicate names
rejected. `--manifest` selects a **sub-directory manifest for multi-source
repos** — source identity is the triple `(git, ref, manifest)`, so one
repository can host several independent sources (harnessator's own skills
ship exactly this way: `--manifest source/harnessator`).

### `harnessator source new [flags]`
Scaffolds a **new component source repository** (the old `harnadd` idea):
manifest + starter rule (self-validated via `--check-paths`), category
directories, README/AGENTS, `git init` + first commit.

| flag | meaning |
|---|---|
| `--name <id>` | **required** — source/package/repository name |
| `--dir <p>` | target directory (default `./<name>`) |
| `--remote gitlab\|azure\|github` | also create the remote via `glab`/`az`/`gh` if present |
| `--description <text>` | package description |

### `harnessator provider use|show|none`
Provider/model governance selection (see [governance](#provider--model-governance)).

```
harnessator provider use <source>:<provider-id>   # set [provider].profile
harnessator provider show                         # current profile + catalog + effective policy
harnessator provider none                         # clear the selection
```

### `harnessator dev <target> [--branch <b>]` and friends
Turns **this project** onto a writable overlay of one source — isolated:
every other project keeps using the pinned read-only checkout.

```
harnessator dev <source> | <source>:<cat>/<id> [--branch <b>]  # start (branch default harnessator/dev/<user>)
harnessator dev status                    # overlays, branch, dirty state
harnessator dev sync [source]             # fetch + rebase onto origin/<ref> (branch refs)
harnessator dev drop <source> [--force]   # unwire + remove overlay (refuses dirty/unpushed unless --force)
```

Implementation: git worktree-free local clone of the store repo, branched
at the pinned commit, `origin` rewired to the real remote so plain git
pushes work. Bindings live in `.harnessator/dev.json` (gitignored).

| flag | meaning |
|---|---|
| `--branch <b>` | dev branch name (start) |
| `--force` | drop anyway despite dirty/unpushed state |
| `--config <p>` | project config path |

### `harnessator contribute [source] [flags]`
For each active dev overlay (or the named one): refuses uncommitted
changes — commit natively in the overlay first — then
`git push -u origin <branch>` and opens the MR/PR through a thin host
layer: `glab` / `az` / `gh` when installed, otherwise it prints the exact
forge URL. Nothing else is host-specific.

| flag | meaning |
|---|---|
| `--title <t>` | MR/PR title (default `harnessator(<source>): dev updates`) |
| `--body <b>` | description |
| `--target-branch <b>` | override the target |
| `--config <p>` | project config path |

After merge: `harnessator update` pulls it into your pinned checkout.

### `harnessator serve [--port <p>] [--config <path>]`
Localhost web GUI — **binds 127.0.0.1 only**, no auth (loopback *is* the
boundary). One page: pins, plan with provenance, governance, dev state,
warnings; buttons for setup/update/verify/doctor streaming the human
report back. API: `GET /api/state`, `POST /api/action {"action":"setup|update|verify|doctor"}`.

### `harnessator tui [--config <path>]`
Interactive terminal menu over the same core:
`[s]etup [u]pdate [v]erify [d]octor [l]ist [e]nable <ref> [x]disable <ref> [w]hy <ref> [q]uit`.
Full-screen polish is tracked as a refinement; it is intentionally
stdlib-only (no new dependency).

## File formats

### `harnessator.toml` — project config (committed)

```toml
kind = "project"
schema = 1
personal = "~/.harnessator/personal.toml"   # optional local fallback layer

targets   = ["opencode", "claude"]          # from: opencode claude codex
                                            #       antigravity pi cursor (MVP);
                                            #       gemini cline copilot windsurf (planned)
templates = ["corp:default", "devops:jira-flow"]   # profiles to expand

[[source]]
name  = "corp"
git   = "https://git.example.com/ai/corp-tools.git"
ref   = "~v1.2"          # tag range or branch (see Ref expressions)
layer = "corporate"      # ascending precedence: corporate|team|project|personal|local

[[source]]
name  = "roland"
git   = "https://git.example.com/users/roland/tools.git"
ref   = "main"
layer = "personal"

[[source]]
name     = "harnessator"
git      = "https://git.example.com/tools/harnessator.git"
ref      = "~v1"
manifest = "source/harnessator"   # sub-dir manifest: multi-source repos

[select.skill]                                # per-category refinement
include = ["corp:example-skill", "draft/*"]   # "*" selects all; bare id binds
exclude = ["corp:slack-skill"]                 #  first source (sorted)

[provider]
profile = "corp:provider/litellm-acme"        # see governance

[policy]
profiles = ["corp:policy/models-approved"]    # listed order = override order

[corporate]                                   # advisory client-side; enforced by CI
allowlist = ["https://git.example.com/ai/**", "https://git.example.com/users/**"]
lock-mode = "strict"                          # strict|soft
```

Top-level scalars **must** precede `[[source]]` (TOML scoping; the loader
rejects the misplaced-scalar trap with `unknown keys: [source.targets …]`).

### `harnessator.toml` — source manifest (in every component repo)

```toml
kind = "source"
schema = 1

[package]
name = "corp-tools"                # how refs address it: corp-tools:rule/x
description = "…"
license = "Apache-2.0"

[[source]]                         # a source may import child sources
name = "team-devops"
git  = "https://git.example.com/ai/devops-tools.git"
ref  = "~v0.3"

[profiles.default]                 # named selections consumed via templates
rule  = ["security-baseline", "git-workflow"]
skill = ["*"]

[component.rule."security-baseline"]
path        = "rules/security-baseline.md"
description = "…"
tags        = ["core", "security"]
always      = true                 # rules only: cannot be disabled
targets     = ["claude", "opencode", "cursor"]  # empty = every target
capabilities = ["network"]         # declared risk surface: network|exec|fs
requires     = ["hooks"]           # needed harness feature (loud skip if absent)

[component.rule."security-baseline".hint.cursor]  # per-adapter hints
globs        = ["**/*.go"]
always-apply = true

[component.mcp."jira"]
path = "mcp/servers.json"          # multi-server convention: one file,
key  = "jira"                      # one component per server via key
```

### `harnessator.lock` (committed, generated — never hand-edited)

```toml
# harnessator.lock - generated by "harnessator update"; do not edit.
schema = 1

[[source]]
name = "corp"; git = "…"; range = "~v1.2"; resolved = "v1.2.3"; sha = "40-hex"

[[component]]
id = "corp:rule/security-baseline"; source = "corp"
sha = "40-hex"; digest = "sha256:…"   # stable content hash of the component path
```

`verify` re-digests everything; edits in the store are drift. Lock files
are byte-stable → clean diffs → reviewable config drift.

### `.harnessator/` in your project (gitignored, generated)

| file | purpose |
|---|---|
| `active.md` | agent-facing index: load ONLY these (the redirect entry into everything) |
| `active.json` | machine-readable plan snapshot |
| `dev.json` | dev-overlay bindings `{source:{branch}}` |
| `tools/<id>/` | link-farm mirrors of directory tools |
| `pi/<source>/` | generated pi-package farms |

### `harnessator.toml` — provider profile (source component `providers/*.toml`)

```toml
name          = "litellm-acme"
default_model = "big"                 # alias or concrete id of [models]
auth_env      = "LITELLM_API_KEY"     # NAME fallback for every style

[models]                              # the approved catalog
big   = "claude-sonnet-5"
fast  = "claude-haiku-5.5"

[styles.openai-compatible]            # one endpoint per API family (>= 1)
base_url = "https://litellm.acme/v1"

[styles.anthropic-compatible]         # gateways expose both families at
base_url = "https://litellm.acme/anthropic"   # different paths
auth_env = "ANTHROPIC_KEY"            # optional per-style override
```

Known API families: `openai-compatible`, `anthropic-compatible`,
`openai-responses`. Endpoints are keyed by **protocol family, not harness
product** — each target's governance *dialect* picks the first family it
can speak (table below). The old product-keyed blocks (`[opencode]`,
`[claude]`, `[codex]`) are rejected as unknown keys.

### policy component (`policies/*.toml`)

```toml
reason         = "approved catalog"
strict         = true               # default true
allowed        = ["big", "smart"]   # aliases or concrete ids (both spellings match)
subagent_model = "big"              # pinned into claude (CLAUDE_CODE_SUBAGENT_MODEL)
```

## References and ids

| form | resolves to |
|---|---|
| `skill/example` | category + component id (bare → first declaring source, sorted by name) |
| `corp:skill/example` | exact source + component |
| `example` | category discovered via fetched manifests (unambiguous or error) |
| `corp:default` | profile "default" of source corp → template |

## Ref expressions

| ref | meaning |
|---|---|
| `main`, `dev`, `v2-migration` | branch name (follows HEAD on update) |
| `*` / `any` | newest non-prerelease tag |
| `1.2.3`, `v1.2` | exact tag (partial = `.0` filled) |
| `~v1.2` | `>=1.2.0, <1.3.0` |
| `~1` | `>=1.0.0, <2.0.0` |
| `^v1.2.3` | `>=1.2.3, <2.0.0` (`^0.y.z` → within minor; `^0.0.z` → within patch) |
| `>=1.2 <2` | conjunction (space or comma) |

Prerelease/build tags never match (plain-release policy for corporates).
`dev sync` requires a branch ref; tag-range sources sync by
contribute + `update`.

## Scope layers

Up to five files - all sharing the project schema - merge into one
effective config, in ascending precedence:

| layer | file | owner |
|---|---|---|
| corporate | `/etc/harnessator/config.toml` (Windows: `%ProgramData%\harnessator\config.toml`) | IT / config management |
| team | `<project>/harnessator.team.toml` | domain owners, committed to the repo |
| project | `<project>/harnessator.toml` | the repository itself |
| personal | `~/.harnessator/personal.toml` | one developer, machine-wide |
| local | `<project>/.harnessator/local.toml` | one developer, THIS project (gitignored) |

Merge semantics: targets and sources union (same source name with two
git URLs is a hard conflict, never a silent pick); templates and select
entries concatenate in layer order; the highest layer naming a `provider`
wins; `policy.profiles` concatenate; the corporate allowlist unions and
any `lock-mode = "strict"` wins. Layers add and steer - they never
silently delete structure. `enable`, `disable` and `source add` route
their writes with `--layer`; provenance names the introducing file
(`[via team]` in `why`/`list`).

### `harnessator layers`
Lists every scope-layer path, present or absent, and what it contributes
(sources, templates, includes/excludes, provider/policy/corporate
markers) - the merge made visible, no hand-reading of five files.

## Provider & model governance

Selection: `[provider].profile` (or `harnessator provider use`); exactly one
enabled provider component may also be auto-adopted, ambiguous selection
warns. Policies: `[policy].profiles` in listed order, later wins per key;
`strict` defaults **true**.

Enforcement: happens inside every command (via the core `Open`) — a strict
violation blocks `setup`/`list`/`doctor` alike with a `model policy
violations (strict)` error; soft violations warn. Rules validated:
allowed entries exist in the catalog; `default_model` and `subagent_model`
are allowed (alias↔concrete both accepted).

Rendering goes through governance *dialects* (`internal/dialect`): a
target has governance iff a dialect is registered for it, and each dialect
picks the first endpoint style it can speak — a profile exposing no
speakable style is a loud skip, never a silent drop.

| target | style priority | what governance renders |
|---|---|---|
| opencode | openai-compatible → anthropic-compatible | `provider` block in `opencode.json` (npm package chosen by style, `baseURL`, `apiKey: "{env:NAME}"`) + `model` default + models map **narrowed to allowed** — the picker itself is the enforcement |
| claude | anthropic-compatible only | `settings.local.json` env: `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_SONNET/OPUS_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL` (tier-alias mapping) and **`CLAUDE_CODE_SUBAGENT_MODEL`** — kills the "sub-agents on an unapproved model" collision |
| codex | openai-compatible → openai-responses | managed block: `model_provider`, `model`, `[model_providers.*]` (`base_url`/`env_key`/`wire_api`: `chat` vs `responses`) |
| pi | no dialect yet | loud skip — endpoint file shape unverified in 1.3.0 |
| cursor | no dialect yet | loud skip — model control is server-side (Team/Enterprise) |
| antigravity | no dialect yet | loud skip |

Secret values **never** appear in configs, the store, lock or generated
artifacts — only env-var names; supply them via environment
(`.devenvironment/env.local.sh` convention).

## Dev & contribute workflow

```
$ harnessator enable corp:skill/my-skill && harnessator setup
$ harnessator dev corp:skill/my-skill
  overlay: ~/.harnessator/dev/corp  branch: harnessator/dev/roland
$ harnessator setup          # THIS project now links into the overlay;
                             # every other project untouched; lock not written
$ cd ~/.harnessator/dev/corp
$ $EDITOR skills/my-skill/SKILL.md
$ git commit -am "make skill assert AC coverage"     # native git workflow
$ harnessator dev sync && harnessator contribute
  corp: MR created (via glab) - branch harnessator/dev/roland
$ ...merge... && harnessator update                  # back to pinned, lock moves
$ harnessator dev drop corp
```

## Environment

| variable | meaning |
|---|---|
| `HARNESSATOR_HOME` | relocate the store (default `~/.harnessator`) |
| provider `auth_env` names (rendered per harness, e.g. codex `env_key`) | e.g. `LITELLM_API_KEY` — consumed by the harness, never by harnessator |
| git auth | standard git mechanisms for the configured URLs (`credential.helper store`, SSH keys, …). Private https sources need a helper configured, else `harnessator setup` fails with git's own message |

## Exit codes

| code | meaning |
|---|---|
| 0 | success |
| 1 | failure (validation, policy violation, drift, git error) |
| 2 | usage error (bad command/flags) |

## CI integration

```yaml
# .gitlab-ci.yml — the gate that makes "works on my machine" impossible
verify:
  image: golang:1.27
  script:
    - go build -o /tmp/harnessator ./cmd/harnessator   # or download a release
    - /tmp/harnessator verify
```

`verify` checks committed lock vs content digests; combined with protected
branches + MR review of lockfile diffs you get auditable agent-config
supply chain, no server component required.

## Troubleshooting

| symptom | fix |
|---|---|
| `no harnessator.toml … run harnessator init` | you're not in a managed project |
| `git clone … could not read Username` | configure `git config --global credential.helper store` (or an SSH URL) |
| `ref "~v9" matches no tag` | source has no release in range; `harnessator source list` shows pins; adjust ref or tag the source |
| `digest drifted` in verify | pinned checkout edited (or store corrupted) — `harnessator update` re-exports; investigate who edited |
| `blocked by non-link file` in doctor | a human file sits at a managed link path — move it, then re-setup |
| `stray managed symlink` | leftovers from a renamed source/older tooling — `harnessator setup` (managed dirs reset each run) |
| harness doesn't see skills | restart it; check `.harnessator/active.md` lists the component |
| claude sub-agent on wrong model | enable a policy with `subagent_model` + provider; re-setup |
| `no provider profile selected` warning | policy is set but provider isn't: `harnessator provider use <source>:<id>` |
| pi ignores everything | first run needs project trust — approve the project in pi |
