# Adopt existing projects (scan → harvest → adopt)

You already have agent config - scattered across `.claude/skills`,
`AGENTS.md` files, maybe a `prompts/` folder, copied between repos until
the copies drifted. harnessator's cold-start tools turn that mess into
a governed source repo **without touching your files until you say so**.

We'll build a small demo sprawl (skip the first block and use your own
paths):

```sh
mkdir -p sprawl/be-app/.claude/skills/api-design sprawl/fe-app/.claude/skills
cat > sprawl/be-app/.claude/skills/api-design/SKILL.md <<'MD'
---
name: api-design
description: REST design conventions for our services.
---
Every list endpoint must paginate. Version paths under /v1.
MD
cp -r sprawl/be-app/.claude/skills/api-design sprawl/fe-app/.claude/skills/api-design
printf 'be variant: adds cursor rules\n' >> sprawl/be-app/.claude/skills/api-design/SKILL.md
printf '# AGENTS\nUse pnpm.\n' > sprawl/fe-app/AGENTS.md
```

## 1. Scan - read-only, always safe

```sh
harnessator scan sprawl/be-app sprawl/fe-app --json > scan-report.json
harnessator scan sprawl/be-app sprawl/fe-app
```

The report groups every recognized component **by content digest**:
identical copies merge into one candidate, diverged copies (our
`api-design` case) are flagged with each location. Nothing is modified;
`scan` also masks potential secrets and flags files outside recognized
layouts. Exit code 0 means "scan completed" - findings are data, not
failures.

## 2. Harvest - publish, review elsewhere

```sh
harnessator harvest sprawl/be-app sprawl/fe-app --name team-corpus --prefer newest
```

This writes a **new source repo** (`team-corpus/`): the components in
canonical layout plus a `harnessator.toml` manifest registering them.
Guarantees baked in: inputs are never modified; every conflict,
sanitize finding and invalid id aborts *before* any write; divergent
copies need an explicit `--prefer newest|largest`; re-runs are
idempotent. Divergent copies that need human judgment land in a
residual report for the guided tail (the `harnessator-harvest` skill
walks an agent through classifying them; the tool then re-validates -
agent drafts, tool ratifies).

Review the output like any generated code — harvest leaves it a clean,
committed git repo:

```sh
cd team-corpus
git log --oneline
cd ..
```

Push it wherever your org hosts sources (a new repo on GitHub/GitLab),
and publish it - it is now a registry source, consumable by anyone.

## 3. Adopt - donors become governed consumers

```sh
harnessator harvest sprawl/be-app sprawl/fe-app --into team-corpus --prefer newest --adopt
```

With `--adopt`, each donor project gets its own `harnessator.toml` +
lock consuming the published corpus, and scattered originals are
replaced by managed links - **but only where content matches the
published digest**. A divergent original is kept in place (and the link
that would overwrite it is skipped loudly). Collisions with generated
paths back up to `*.pre-adopt`. Nothing is ever destroyed by policy.

## 4. Verify the result

```sh
cd sprawl/fe-app
harnessator list
harnessator check --json
```

Each project is now a thin consumer: config + lock in VCS, content
shared and pinned from the corpus. Fix a skill once in `team-corpus`,
every project picks it up by bumping the lock in review - the drift you
scanned in step 1 can structurally not come back.

## Notes for real projects

- Run step 2 against **all** your repos at once - cross-project
  duplicates are the point; harvest merges them by digest.
- If a path isn't a git repo yet, adopting still works; it writes the
  consumer files and you commit them yourself.
- Loose single files (not under a recognized layout dir) show up as
  residuals rather than being guessed into a category - classify-never-obey.
