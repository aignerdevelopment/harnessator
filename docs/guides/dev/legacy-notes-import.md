# One AGENTS.md to rule them all (until it rules nothing)

Every agentic codebase of a certain age has *the file*: one heroic
`AGENTS.md`, grown section by section like tree rings - three genuine
engineering laws, a grudge against a linter, someone's "always use
pnpm!!" and the coffee policy. Every harness was supposed to read it.
Most of them read each other's files instead, and the coffee policy
went unenforced.

This is the story of turning that file into governed content. It has
three ramps, cheapest first. The tool shows you all three, in order,
because "0 components" is not an answer - it is a dare.

## The scan talks back (it learned)

```sh
mkdir -p legacy && cat > legacy/AGENTS.md <<'MD'
# Team rules

## Always test before commit
Run the suite. No exceptions. Yes, yours either.

## Commits are cheap, pipelines are not
Use the commit helper; one concern per commit; bump the version once.
MD
harnessator scan legacy
```

```console
legacy  [unmanaged]  0 component(s), ..., 1 residual, 0 near-miss
  residual: AGENTS.md - unmanaged agent note file - likely rule content:
            import it (harvest --with-notes), have an agent classify it,
            or split by hand into rules/<id>.md
```

Note the verbs: *import*, *classify*, *split*. Pick your poison.

## Ramp A - programmatic: headings become rules, one command

```sh
harnessator harvest legacy --name legacy-tools --with-notes
```

```console
harvest -> .../legacy-tools: 2 add, 0 skip-identical
  add  rule  always-test-before-commit     .../legacy/AGENTS.md § Always test before commit -> rules/always-test-before-commit.md
  add  rule  commits-are-cheap-pipelines-are-not ... § Commits are cheap, pipelines are not -> rules/commits-are-cheap-pipelines-are-not.md
```

Each rule keeps its section verbatim, and the provenance line does
archaeology for you: `AGENTS.md § Always test before commit`. The split
is a *layout heuristic* - headings are where thoughts park, not always
where they live. A file with no decodable headings imports as ONE
verbatim rule rather than getting an interpretive dance. Review the
result; `harnessator validate --check-paths legacy-tools/harnessator.toml`
makes the review cheap. (Yes, it also nagged about the missing LICENSE -
it nags about everything important, that's the product.)

## Ramp B - with judgment: make an agent do the reading

When the file is a sedimentary record, `--with-notes` alone is a paper
tiger. So the tool packages the judgment into a file - one command
leaves a self-contained agent brief beside the fossils:

```sh
harnessator kickstart legacy
```

The file it writes - `HARNESSATOR-KICKSTART.md` - carries the live scan
findings, the mission ("Read the note files. Never edit the fossil -
someone's feelings live in it. One rule file per DISTINCT concern;
strip secrets and list what you removed; park project-local trivia in
90-local-notes.md; order identity/safety first"), and the exact gate
commands the tool owns. Then the one line that starts everything,
pasted to whichever agent you trust most:

    Work through HARNESSATOR-KICKSTART.md - report before changing anything.

The agent drafts; the tool ratifies. Every shortcut above ends in the
same gate: `harvest` refuses conflicts and loud sanitize findings, the
manifest self-validates, and re-runs are idempotent.

## Closing the loop: the file that told everyone what to do, governed

```sh
cd legacy && git init -b main && git add -A && git commit -qm seed
SRC="$(cd ../legacy-tools && pwd)"
harnessator init --targets claude --source "file://$SRC" .
harnessator enable legacy-tools:default
harnessator setup
harnessator why rule/always-test-before-commit
```

The hand-written `AGENTS.md` survives setup untouched - harnessator
never clobbers foreign files; retiring the original is a human commit
away, which is exactly how it should be for the file whose entire job
was telling other people what to do. From here it's the normal
lifecycle: rules evolve in the source, every consumer moves by
`harnessator update` and a reviewable lock diff. The coffee policy,
meanwhile, remains unenforced, as it should be.

## Honest footnotes

- Heading-splitting reads `#` levels; code fences are respected (a
  `##` inside a snippet is prose about prose, not a rule).
- Ids are slugged from headings (`-` and lowercase, capped) - two
  sections with the same heading import as `name` and `name-2`; a real
  collision aborts rather than picking favorites.
- `--with-notes` on a tree that *also* holds recognized components
  imports both; residuals keep showing until they are genuinely gone.
- Windows, CRLF, backslash paths: tested - the adoption report that
  produced this page arrived from exactly such a machine.
