# Quickstart: a new project, governed from minute one

Ten minutes from empty folder to a repo whose agent configuration is
pinned, reviewable and drift-checked. This guide's blocks run as
written; the example uses the public reference corpus.

## 1. Create the project

```sh
mkdir my-app && cd my-app
git init -b main
```

## 2. Initialize harnessator

Pick the harnesses your team actually uses. `--source` registers a
component source you will inherit everything from:

```sh
harnessator init --targets claude,opencode \
  --source https://github.com/aignerdevelopment/agentictools.git .
```

This wrote two committed files: `harnessator.toml` (what you want) and
the basis for `harnessator.lock` (what you got, pinned by commit and
content digest).

## 3. Choose a baseline

Sources publish *profiles* - named bundles of components. The reference
corpus ships `minimal` (the core rules) up to `max`:

```sh
harnessator enable agentictools:minimal
harnessator setup
```

`setup` fetched the source, resolved the plan and materialized each
harness's own layout (symlinks into the pinned store plus generated
config files). Restart your harness to pick them up.

## 4. See what you now have

```sh
harnessator list
harnessator why rule/00-agentictools-source-of-truth
```

`list` shows every enabled component with its provenance; `why` answers
the "who decided this?" question for any single one.

## 5. Make it reproducible

```sh
harnessator check --json
git add harnessator.toml harnessator.lock
git commit -m "govern agent config with harnessator"
```

Commit the config **and the lock**. From now on, every clone of this
repo resolves to the exact same component content - digests verified -
no matter which machine or whose laptop. `check` is the one-command CI
gate; add it to your pipeline (see
[ci-and-review](../platform/ci-and-review.md)).

## What next?

- **Add a harness later** (the team adopts Codex): edit `targets` in
  `harnessator.toml` or re-run `harnessator init` - components don't
  change, only their renderings. See
  [switch-harness](switch-harness.md).
- **Bring your existing skills/rules** instead of starting empty:
  [adopt-existing-projects](adopt-existing-projects.md).
- **Company gateway / approved models**: 
  [configure-gateway-access](configure-gateway-access.md).
- **Edit something live and give it back upstream**:
  [day-in-the-life](day-in-the-life.md).
