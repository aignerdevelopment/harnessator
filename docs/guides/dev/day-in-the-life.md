# A day in the life

You adopted harnessator; the config is committed; your team reviews
lockfile bumps like dependency updates. This is what an ordinary week
looks like.

## Pulling in upstream improvements

Sources move (new skill versions, fixed rules, a retired prompt). Review
and take them like any dependency bump - below we first create a managed
project so the whole guide runs end-to-end:

```sh
git init -b main
harnessator init --targets claude --source https://github.com/aignerdevelopment/agentictools.git .
harnessator enable agentictools:minimal
harnessator setup
harnessator update
harnessator list --all
git diff harnessator.lock
```

`update` re-resolves your ref expressions, fetches, rewrites the lock -
and `git diff` on it is the review: exactly which components changed,
each with old/new sha256 content digest. No fetching happens during a
plain `setup` when the lock is current.

Commit the lock, open the MR. Reviewers see a one-file diff, not a
directory of silent overwrites.

## Editing a skill live, without going rogue

You need a real change to a shared skill *now*, for your project:

```sh
harnessator dev agentictools
```

This rewires **only your project** to a writable overlay checkout on a
dedicated branch (`harnessator/dev/<source>`) in the store - other
projects keep using the pinned read-only content. Now edits in your
harness dirs are live: test them in your agent immediately.

```sh
harnessator dev status
```

## Giving it back

When the edit proved out, push the branch and open the merge request in
one step (needs push access to the source, so shown rather than run):

```console
harnessator contribute agentictools --title "skill: tighten commit hygiene rules"
```

Native git under the hood: branch, push, MR via `glab`/`az`/`gh` (or a
precise web URL if you have none). Once it's merged upstream, everyone
- including you - gets it through a normal `harnessator update`, and
your overlay is obsolete:

```sh
harnessator dev drop agentictools
harnessator update
```

## Task toolchains

Different kind of work, different toolchain - without touching anyone
else's config:

```sh
harnessator enable agentictools:skill/example-skill --layer local
harnessator setup
harnessator layers
```

Those picks go to `.harnessator/local.toml` (gitignored, this project
only), while `~/.harnessator/personal.toml` (`--layer personal`) carries
your favorites across every project. Both are visible in `harnessator
layers`, and `why` names the file. See
[corporate-registry](../platform/corporate-registry.md#task-toolchains-the-local-layer).

## When someone (you) edits managed content directly

Managed files are symlinks into the store or spliced blocks with
`>>> harnessator managed` markers. The one thing nobody may do is hand
edit them - and the tool notices:

```sh
harnessator verify
harnessator doctor
```

`verify` compares on-disk state to the lock: checkout markers, per
component digests, dev-mode exceptions. Exit 1 on drift - that's the
same command your CI runs. `doctor` is the human version: freshness,
adapter spec diffs (missing, stray, blocked links), gitignore hygiene.
The recovery for either is usually `harnessator setup`, and the real
fix is whatever channel (dev mode) the edit was meant to travel.

## A new teammate clones the project

They need nothing but the binary and network access to the sources:

```sh
harnessator setup
```

Config + lock in the repo pin exact commits and digests; setup
reconstructs the identical harness layout on their machine, their OS.
That's the whole onboarding.
