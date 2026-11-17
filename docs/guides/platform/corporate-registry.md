# Building the corporate registry

Whoever owns the company's agent setup operates a *registry*: reviewed
component sources, layered scoping, and a supply chain that CI
verifies. This guide is the platform-side blueprint; the running demo
of every command here is the public reference corpus
[`aignerdevelopment/agentictools`](https://github.com/aignerdevelopment/agentictools).

No server to run: the registry is a **format**, sources are git repos,
and "publishing" is a git push.

## 1. The corporate source repo

A source is ordinary content in a repo plus a manifest registering it:

```sh
harnessator source new --name acme-governance --dir acme-governance \
  --description "Acme's shared agent rules, skills and policies"
cd acme-governance
```

Scaffolded with a valid manifest (`harnessator.toml`) and example
components. The manifest is the registry contract - identity triple
`(git, ref, manifest path)`, a `schema` version, and one component
entry per unit:

```toml
[component.rule."security-baseline"]
path = "rules/security-baseline.md"
description = "Mandatory security rules for all Acme agents."
tags = ["core", "security"]
always = true                      # no project selection may drop it

[profiles."default"]
rule = ["00-authoring", "security-baseline"]
```

Categories: rules, skills, prompts, workflows, agents, hooks, tools,
MCP definitions, **providers** and **policies** (see
[model-governance](model-governance.md)). Validate before pushing:

```sh
harnessator validate harnessator.toml
git log --oneline -1
```

Push to the org's GitLab/GitHub - `source new` already made the initial
commit; that push is the publish step. Host it like
any internal library repo: CODEOWNERS on the manifest, protected
branches, MR review. A component is "approved" when it's merged,
because the lockfile digest is what consumers pin.

## 2. Versioning: refs, not registries

Consumers address sources by ref expressions - branch (`main`, follows
HEAD) or tag ranges (semver grammar: `v1.2`, `~v1.2`, `^v1.2.3`).
Release discipline is git tags:

```console
$ git tag v1.0.0 && git push origin v1.0.0
```

- Pilot teams track `main` (latest reviewed).
- Production projects pin `~v1` (patch-level safety, minor bumps are
  reviewable `harnessator update` diffs).
- Prerelease tags never match - plain releases only, by design.

## 3. Layering: corporate → team → project → personal

Every project config resolves from up to four layers, highest
precedence last:

| layer | file | who writes it |
|---|---|---|
| corporate | `/etc/harnessator/config.toml` (Windows: `%ProgramData%\harnessator`) | IT/MDM distributes it; the allowlist + lock-mode live here |
| team | `harnessator.team.toml` beside the project config | domain owners, committed to the repo |
| project | `harnessator.toml` | repo owners - your daily file |
| personal | `~/.harnessator/personal.toml` | each developer, machine-wide (put it in a dotfiles repo and symlink - it is just a file) |
| local | `.harnessator/local.toml` | each developer, **this project only** - the per-task toolchain switch, gitignored by construction |

Every layer except the project anchor is optional, and all share the
same schema. The merge is additive: sources union (same name, different
URL = hard conflict), templates and selects concatenate in precedence
order, the highest layer naming a provider wins, any strict corporate
lock wins. Layers add and steer - they never silently delete structure.

```sh
mkdir app && cd app
harnessator init --targets claude .
harnessator source add https://github.com/aignerdevelopment/agentictools.git --layer team
harnessator enable agentictools:minimal
harnessator setup
harnessator why rule/00-agentictools-source-of-truth
harnessator layers
```

`why` is the audit trail: it names the layer and selector that made a
component active (`[via team]` when the team file introduced it), so
"who enabled this" is never folklore. The lock records the resulting
flat, digested truth.

### Task toolchains (the local layer)

The same engineer running an embedded Linux board on Monday and a UI
spike on Tuesday wants a different toolchain per project - without ever
touching shared config:

```console
$ cd ~/work/embedded-app
$ harnessator enable acme-embedded:skill/board-flash --layer local
$ cd ../ux-spike
$ harnessator enable acme-ux:skill/design-review --layer local
```

The picks land in each repo's `.harnessator/local.toml` (gitignored),
bind to that project only, and show up in `harnessator layers`. Two
tasks, two toolchains, same corporate sources - zero crosstalk, and the
next engineer's `why` says exactly where each pick came from.

## 4. Rollout order that doesn't hurt

1. **Bootstrap from what exists.** Run
   [scan/harvest](../dev/adopt-existing-projects.md) across teams'
   repos - the diverged-copy report *is* your migration backlog, and
   harvest's output is already source-shaped.
2. Seed the corporate source with the uncontroversial core (house
   rules, the `always` set), publish, pin it.
3. Projects adopt with profiles; teams add layer sources as they
   accumulate ownership of domains.
4. Wire `harnessator check` into CI ([ci-and-review](ci-and-review.md))
   so unmanaged drift fails pipelines, not audits.

## 5. Governance lives in the same registry

Provider catalogs and model policies are *components like any other* -
same review, same pinning, same layering. The enforcement story is its
own guide: [model-governance](model-governance.md).
