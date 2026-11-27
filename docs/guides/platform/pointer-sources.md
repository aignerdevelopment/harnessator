# Borrow, don't mirror (pointer sources)

Some of the best agent content in the world lives in repositories that
never asked to be package sources - and asking them to add a manifest is
a politely worded "no, thanks" (the superpowers saga, see the diary of
your fears). Mirroring works but costs: a second home for someone
else's bytes, sync crons, deploy keys, and a license review *per repo*.

A **pointer source** flips the direction: the manifest stays in *your*
repo, the content stays in *theirs*, and harnessator pins both. Nothing
is copied, so there is nothing to keep in sync - and pointing is not
redistributing, which turns the license question from "may we mirror
this?" into the much simpler "may we recommend this?".

## 1. Six lines of intent

```sh
harnessator source new --name acme-skills --dir acme-skills >/dev/null
mkdir -p foreign/skills/ship-in-small-steps foreign/skills/twice-shipped
printf -- '---\ndescription: Ship in small, reviewable steps.\n---\n' > foreign/skills/ship-in-small-steps/SKILL.md
printf -- '---\ndescription: Prove it twice.\n---\n' > foreign/skills/twice-shipped/SKILL.md
git -C foreign init -qb main && git -C foreign add -A && \
  git -C foreign -c user.name=t -c user.email=t@t commit -qm seed
```

That `foreign/` repo is the stand-in for obra/superpowers - a real
repository with skills, no manifest, no interest in either. Now the
pointer manifest, source format v1.2 (`schema = 3`):

```sh
cd acme-skills
cat > harnessator.toml <<TOML
kind = "source"
schema = 3

[package]
name = "acme-skills"

[content]
git = "file://$(pwd)/../foreign"
ref = "main"

[discover]
skill = "skills/*"

[profiles]
[profiles.default]
skill = ["*"]
TOML
git add -A && git -c user.name=t -c user.email=t@t commit -qm "pointer manifest (the foreign/ tree lives beside the repo, never inside it - pointing, not mirroring)"
```

`[content]` says: my paperwork, their bytes. `[discover]` now runs
against **their** tree - every skill directory auto-registers at load
time, so an upstream release cannot make this file stale. (The old
binary reading `schema = 3` fails loudly with "unknown keys", never
half-interprets it. Schema discipline, as always.)

## 2. Consume it like any source

```sh
cd .. && harnessator init --targets claude --source "file://$PWD/acme-skills" --ref main proj
harnessator enable acme-skills:default --config proj/harnessator.toml
harnessator setup --config proj/harnessator.toml
harnessator list --config proj/harnessator.toml
```

```console
skill      acme-skills:skill/ship-in-small-steps  [template acme-skills:default [via project]] (import-only)
skill      acme-skills:skill/twice-shipped  [template acme-skills:default [via project]] (import-only)
```

The lock pins **both** commits - `sha` (manifest) plus `content_git`,
`content_range`, `content_resolved`, `content_sha` - so `harnessator
update` diffs both and `check` verifies both trees offline. When the
foreign repo moves, discovery re-runs against the fresh content
checkout: upstream growth appears on `update` with zero edits to the
pointer manifest. In the real world this is the sentence where you put
`git = "https://github.com/obra/superpowers"` and `ref = "~v7"` and go
do something else forever.

## 3. Import-only is a property of the content, not a punishment

`dev` on a pointer source refuses, and the refusal is the ramp:

```console
dev: acme-skills is a POINTER source - the content lives in file:///…/foreign,
which you do not own.
  to tweak a component locally: harvest its directory from the pinned
  content checkout into a source you own (the origin records the vendoring),
  then disable theirs, enable yours, run setup.
  to fix it for everyone: PR upstream - every pinned consumer gains the fix
  at its next update.
```

Three sanctioned moves, all of them existing commands:

| want | do |
|---|---|
| consume as-is | `enable acme-skills:…` - pinned, digest-checked, offline-verifiable |
| yours, tweaked | harvest that skill directory out of the store checkout into a corpus you own; `disable` theirs, `enable` yours, `setup` uninstalls the replaced content cleanly (verified) |
| upstream, fixed | a PR to the foreign repo; `update` then hands the fix to every consumer - yours included |

Copy-and-own is *vendoring*, and the tool says so: harvested components
carry the provenance, and the license `ATTENTION` you already know from
harvest fires its "verify redistribution terms" nag - because *there*
you really did copy bytes.

A direct (non-pointer) source you still may not edit? Declare it:
`editable = false` in the project's `[[sources]]` entry. Same refusal,
same badges - just consumer-declared instead of structurally derived.
(Say it truthfully: a pointer with `editable = true` is an error, not a
feature request.)

## 4. Honest footnotes

- The content clone **borrows the source's name** in the store
  (`sources/<name>/content/`): re-pointing it to another repo hits the
  same identity guard as the manifest half, and `source forget <name>`
  resets both halves at once.
- `[content]` without `[discover]` is fine (explicit components resolve
  against the content tree too); a component path that lives only in
  the *manifest* repo does not exist for a pointer - validation says so.
- Discovery misses an upstream rename loudly: a `skill = "skills/*"`
  that matches nothing surfaces as the same residual/ramp material as
  every other dead end (`harnessator kickstart .`).
- Cloning a whole foreign repo is bandwidth; a partial/`blob:none`
  fetch is on the performance list, not the correctness one.
- Pointing is not owning: your tags, descriptions, profiles and
  selections remain yours to edit (that's the curation half); the bytes
  are theirs, forever `import-only`.
