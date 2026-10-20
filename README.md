<div align="center">

<img src="assets/harnessator-logo.png" alt="harnessator" width="240" height="185"/>

# harnessator

**Terraform for AI coding agents.**

Your whole team's AI coding setup, as code.
Rules, skills, MCP servers, models and endpoints - versioned in git,
reviewed in merge requests, pinned by digest, applied to every harness.

*one CLI · six harnesses · three OSes · zero servers*

*created and maintained by [Roland Aigner](https://www.aignerdevelopment.com)*

</div>

---

Your engineers use Claude Code, Codex, opencode, Cursor, pi and
Antigravity. Each wants its own config file in its own folder, nobody's
setup matches anyone else's, the approved model list lives in a wiki
nobody reads, and "works on my machine" now applies to your agent
configuration too.

**harnessator is the missing package manager layer for AI coding
harnesses.** Declare once what your org-standard agent setup looks like -
which rules, which skills, which MCP servers, which models your LiteLLM
gateway actually serves - and *apply, verify and roll out that config to
every project, every harness and every OS, reproducibly.* The registry is
git. Access control, review and provenance come from your forge. There is
no server to run.

```sh
harnessator init --source https://git.acme.corp/ai/corp-tools.git --ref '~v1'
harnessator enable corp-tools:default
harnessator setup          # six harnesses configured, lockfile pinned, done
```

## What it does

|  |  |
|---|---|
| 📦 **Components as packages** | rules · skills · prompts · workflows · sub-agents · hooks · tools · MCP servers - each typed, tagged, addressed as `source:category/id`, distributed from normal git repos (manifests included, `harnessator source new` scaffolds one) |
| 🎯 **Six harnesses, one source of truth** | honest per-harness adapters for **Claude Code, opencode, Codex, Antigravity, pi and Cursor** (more planned): native slots where they exist, **loud skips** where they don't - never a silent drop |
| 🖥️ **Every OS** | Linux and macOS via symlinks; **Windows without admin** via NTFS junctions + digest-verified copies. Cross-build matrix is green in CI |
| 🔒 **Reproducible by construction** | committed `harnessator.lock` pins exact commits + sha256 content digests. `harnessator update` is a lockfile diff; review it like any dependency bump |
| 🏛️ **Org structure, not flat files** | five scope-layer files merge into one plan - corporate (`/etc/harnessator`), team, project, personal and per-project local (the task-toolchain switch); `--layer` routes writes, `harnessator layers` shows the merge, `why` names the introducing file |
| 🧑‍💼 **Model & endpoint governance** | first-class provider profiles (LiteLLM/OpenRouter/self-hosted gateways): approved model catalogs with aliases, endpoints keyed by API family rendered through per-harness governance dialects, **sub-agent model pinning** (the `CLAUDE_CODE_SUBAGENT_MODEL` problem, solved) |
| 🚪 **Two-way contribution** | edit a skill in a project (`harnessator dev`), keep working live and isolated, `harnessator contribute` pushes the branch and opens the MR - via `glab`/`az`/`gh` or a precise web URL. GitLab and Azure DevOps are first-class; GitHub lands with open-sourcing |
| ✅ **CI gate for agent config** | `harnessator verify` fails when installed state diverges from the lock - in CI, that's an auditable supply chain for your whole agent setup |
| 🔍 **Publish what you already have** | `harnessator scan` finds scattered skills/rules/prompts in project trees with cross-project *divergence* by digest and masked secret/path findings; `harnessator harvest` turns them into a validated, git-ready source repo - the drift you described, made visible and fixable in one command pair |
| 🖥️ **Beyond CLI** | `harnessator serve` = localhost web GUI (plan, provenance, governance, one-click lifecycle); `harnessator tui` = interactive menu |
| 🔑 **Secret-safe** | generated configs never contain a secret value: only *names* of env vars (`env_key = "LITELLM_API_KEY"`), following the repo-local `.devenvironment` convention |

## The ecosystem idea

harnessator is the tool; the *point* is the ecosystem. Team A builds a
great Jira skill. Team B shouldn't copy-paste it - they should
`enable` it from Team A's source repo, pinned, versioned, with the
option to contribute fixes back via MR. Corporate roots, team repos and
personal overlays all compose the same way. It's what npm did for
libraries, applied to the new artifact class every team is accumulating:
**agent configuration.** The registry is git, so adoption doesn't
require anyone to trust a new central service.

```
corporate source  ─┐
                   ├─► resolution (layers, profiles, precedence, provenance)
team source      ──┤        │
                   │        ▼
personal source  ──┘   lock (commits + digests)  ─►  adapters  ─►  your harness
```

## Install

```sh
# install or update (re-run any time; verifies sha256 against the release):
curl -fsSL https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.sh | bash
```

```powershell
# Windows (PowerShell 5.1+; same checksum discipline):
irm https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.ps1 | iex
```

Installs to `~/.local/bin` (no admin, no root). Pin with
`--version v0.2.0-beta`, relocate with `--dir`. Prefer building from
source or plain downloads? [BUILDING.md](BUILDING.md). Same one-liners
work in the other direction: re-running *is* the update story.

## Quick tour

```sh
harnessator init --source https://git.example.com/ai/corp-tools.git \
                 --ref '~v1' --targets opencode,claude .
harnessator enable corp-tools:default        # a governed profile of components
harnessator search jira                      # what exists across all sources?
harnessator why rule/security-baseline       # ...and why is it on?
harnessator setup                            # apply everywhere, write the lock

# improve a skill in place, isolated to this project, upstream the fix:
harnessator dev corp-tools:skill/jira-issue
harnessator setup                            # links now follow the overlay
$EDITOR ~/.harnessator/dev/corp-tools/skills/jira-issue/SKILL.md
git -C ~/.harnessator/dev/corp-tools commit -am 'assert AC coverage'
harnessator dev sync && harnessator contribute   # MR opened; merge; then:
harnessator update                               # flows back as a pinned change
```

## Docs

|  |  |
|---|---|
| **[USAGE.md](USAGE.md)** | every command, flag, format, sample |
| **[BUILDING.md](BUILDING.md)** | prerequisites, cross-OS builds, packaging, CI |
| **[man/harnessator(1)](man/man1/harnessator.1)** · [harnessator.toml(5)](man/man5/harnessator.toml.5) · [harnessator.lock(5)](man/man5/harnessator.lock.5) | the same, in your terminal (`make install-man`) |
| **[docs/guides/](docs/guides/README.md)** | start at [JOURNEYS](docs/guides/JOURNEYS.md) - four personas, four walks - then the task-shaped guides: first project, existing sprawl, gateway access, corporate rollout, honest security review |
| **[CHANGELOG.md](CHANGELOG.md)** | the development diary: why the tool is shaped the way it is |

## Status & roadmap

**Alpha** - the lifecycle (`init → enable → setup → dev → contribute →
verify → update`) and six adapters are implemented and dogfooding *this
very repo* (it configures itself), but no public API is stable yet.

The dogfood is real: `harnessator.toml` and the committed `harnessator.lock`
at the root are maintained *with* harnessator, pinned to the published
corpus ([aignerdevelopment/agentictools](https://github.com/aignerdevelopment/agentictools)) -
clone this repo, run `harnessator setup`, and it reconstructs its own
harness configuration. `source/harnessator/` is a second public source
(`harnessator source add <clone-url> --manifest source/harnessator`) - the
multi-source example, shipped inside the product tree.

Shipped toward beta: the local-first registry index (`harnessator
index build|search|show`), an MCP server agents can query their own
configuration through (`harnessator mcp serve`), an offline
policy-enforcement gate with git-hook install (`harnessator check`), and
five scope-layer files (corporate → team → project → personal → local)
whose merge `harnessator layers` makes visible; the one-liner installers
are sha256-verified and CI-tested against the live release. Live on
GitHub - remaining: the `harnessator.org` homepage,
expansion adapters (Gemini CLI, Cline, Copilot, Windsurf - one governance
dialect each), serve gallery view. Licensed under **Apache-2.0** (see
[LICENSE](LICENSE)) - **contributions, forks and loud opinions
welcome**: open an issue or a merge request.

## License

Apache License 2.0 - see [LICENSE](LICENSE) and [CONTRIBUTING.md](CONTRIBUTING.md).

## Why not…?

- *rulesync / AGENTS.md symlinking* - great converters; no registry, no
  locking, no org layers, no endpoints governance, no contribution loop.
  Interop is planned, not contested.
- *native plugin marketplaces (e.g. Claude Code)* - single harness by
  design; harnessator treats *all* harnesses as install targets.
- *vendoring one agent-config repo per project* - the thing you're
  already trying to avoid: it drifts. That's the whole point of pinning.
- *Terraform itself* - the analogy we own: same workflow (declarative
  config, reviewed lockfile, drift detection), none of the baggage -
  no cloud API, no provider plugins, no state backend. Git is the only
  server.

---

<sub>⚠️ **Pre-release**: expect sharp edges, read CHANGELOG.md before
upgrading between tags, and keep `harnessator verify` in CI.</sub>
