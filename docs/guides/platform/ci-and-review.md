# CI and the review workflow

The governance claim is: agent config changes travel as reviewed,
pinned, verifiable diffs. That only bites when pipelines enforce it.
Two surfaces: one command (`harnessator check`) and two hooks.

## `harnessator check` - the pipeline gate

One offline command, one exit code: it validates strict model policy,
lock/digest integrity and installed-state drift together. No fetching
- lock pins only, so it runs on every PR in seconds. (The first lines
create a managed project so this guide runs standalone.)

```sh
git init -b main
harnessator init --targets claude --source https://github.com/aignerdevelopment/agentictools.git .
harnessator enable agentictools:minimal
harnessator setup
harnessator check --json
```

GitHub Actions:

```yaml
- name: Agent config gate (harnessator)
  run: |
    curl -fsSL https://github.com/aignerdevelopment/harnessator/releases/latest/download/harnessator_linux_amd64.tar.gz | tar xz
    ./harnessator check --json
```

GitLab CI:

```yaml
harnessator-check:
  stage: test
  image: golang:1.26
  script:
    - go install harnessator.org/harnessator/cmd/harnessator@latest
    - harnessator check --json
```

(`go install`/`latest` pins to the newest published release; teams that
need reproducibility vendor the binary version into their image.)

## Pre-commit / pre-push hooks

For enforcement before the pipeline ever sees it:

```sh
harnessator check --install-hook pre-commit
harnessator check --install-hook pre-push
```

The hook block is unmistakably marked and self-skips in unmanaged
repos, so committing the hook into the repo is safe for the whole team.
`--remove-hook` removes only harnessator's own block - it refuses to
clobber or delete foreign hooks, same honesty the adapters practice.

## What reviewers actually review

1. **The lock diff.** `harnessator update` rewrites `harnessator.lock`;
   its diff is exactly "which components changed, from what digest to
   what digest". Source repo + lockfile pin = the audit trail. A lock
   diff with no accompanying config change means upstream moved
   *without* asking - which is why refs are tag-ranges in production.
2. **The config diff.** New/removed components, layer overrides,
   targets changes, provider/policy selections.
3. **`why` for anything questionable.** `harnessator why skill/foo`
   names the layer and selector that enabled it.

Fail-modes `check` catches for you: an installed file diverged from the
lock (someone hand-edited a managed artifact), a policy violation
(strict), a lock that no longer matches committed config. The recovery
in each direction is explicit: `setup` to accept the lock, or
`update` + commit + review to move it.

## Layering the gates

Where each check belongs:

| where | command | catches |
|---|---|---|
| local dev loop | pre-commit hook | forgetting to regenerate |
| CI per PR | `check --json` | unreviewed governance/config drift |
| scheduled / on-push to main | `harnessator update` + diff | upstream source changes reaching you *as a review*, not a surprise |
| registry side | source repo's own CI (`validate`, review + tags) | breaking changes to shared components |

## Related

- [corporate-registry](corporate-registry.md) - source-side pipeline
- [security-review](security-review.md) - what these gates do and don't protect
