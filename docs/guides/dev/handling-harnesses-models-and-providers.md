# Handling harnesses, models and providers (day-to-day)

You work across harnesses, their models arrive through gateways, and the
keys live wherever each harness decided to put them. Governance
([configure-gateway-access](configure-gateway-access.md)) is how the
company pins that truth down; this page is the **operating** side: what
is configured, what the gateway actually serves right now, what happens
when the two disagree, and who may write where.

Five verbs, one map:

| verb | direction | touches |
|---|---|---|
| `provider use` / `none` | governance → project | the plan (lock) |
| `provider show` | report | nothing |
| `provider import` | live configs → governed source | a new source dir |
| `provider modelupdate` | gateway reality → live configs | harness configs (with backup) |

`import` lifts reality into governance; `modelupdate` pushes truth into
configs. Same keys, opposite directions - and neither ever prints a
key.

## 1. What does this project run on?

Start from the governed side (identical setup to
[configure-gateway-access](configure-gateway-access.md), minus the
policy for brevity):

```sh
mkdir hp && cd hp
harnessator init --targets claude,opencode \
  --source https://github.com/aignerdevelopment/agentictools.git .
harnessator enable agentictools:provider/litellm-demo
harnessator provider use agentictools:provider/litellm-demo
harnessator setup
harnessator provider show
```

`show` answers per target - which harness speaks which style of this
profile, and what happens to the ones that cannot. There is **no
override per harness** by design: one project, one provider truth.

## 2. The gateway is the truth: modelupdate

Model catalogs rot: gateways add, rename and drop models; harness
configs keep whatever was last pasted in. `modelupdate` re-probes the
gateways **named in each harness's own config** - using the keys from
wherever that config keeps them - and refreshes the config to reality:

```sh
harnessator provider modelupdate opencode --dry-run    # explicit harness
harnessator provider modelupdate "*" --dry-run         # quoted star
harnessator provider modelupdate --all --dry-run       # shell-proof star
```

Always dry-run first - the same doctrine that kept every release honest.
On a machine without harness configs the sweep is a full account, not a
shrine:

```console
opencode: no live config (~/.config/opencode/opencode.json{c} or ./opencode.json{c})
claude: no live config
codex: no live config at ~/.codex/config.toml
gemini: native Google API - no known style mapping to probe; report skipped
cline: provider state schema is moving; writing it is deliberately off in v1
copilot: nothing local to probe (server-side or unmapped)
cursor: nothing local to probe (server-side or unmapped)
antigravity: nothing local to probe (server-side or unmapped)
pi: nothing local to probe (server-side or unmapped)

modelupdate: 0 configured, 9 report-only, 0 error(s)
```

On a real machine, lines become per-provider diffs (`+ added
/no-longer-served - kept =`), and where the harness has a writable
catalog the update is **applied in place** beside a timestamped
backup: the write is confined to that provider's model list, so
hand-written comments (the ones explaining *why* a key lives in the
auth store, for instance) survive byte-identical. If a file's shape
cannot be spliced safely, the tool falls back to a full rewrite -
which loses comments and says so loudly.

## 3. When the diff goes wild: the mass-removal guard

The guard was born from a real first run: a gateway answering
`openrouter/deepseek/v4` while the config spoke `deepseek-v4` made the
naive diff propose **467 removals of 506 entries** - gutting a working
catalog on a naming technicality, not a fact. Since then:

> removals that would cut more than a quarter (and 20+) of a catalog are
> **refused**, with the counts and a paste-ready diagnosis.

```console
  MASS REMOVAL REFUSED: 467 of 506 configured entries missing from the gateway response. Two honest readings: the gateway renamed its id namespace (mirror ids vs curated names), or it GENUINELY serves less now (a changed gateway mapping is exactly this shape - e.g. switching from an openrouter/* mirror to curated models). Verify which: harnessator provider modelupdate opencode --verbose --dry-run - compare the id SPACES, not just the counts. If the removals are real, apply: harnessator provider modelupdate opencode --allow-removals
```

Both readings are real possibilities and *you* know your gateway: a
deliberate curation of the mapping (mirror → selected models) produces
exactly this diff, and it is correct - `--allow-removals` applies it.
The guard exists so that *nobody* applies a mass deletion on faith
without seeing the two readings first.

So when it fires, the next step is a *read*, not a flag:

```sh
harnessator provider modelupdate opencode --verbose --dry-run
```

`--verbose` also lifts the 10-id cap on diff lines (summaries by
default; full lists opt-in). `--allow-removals` is the deliberate
opt-in past the guard - use it only when you have confirmed the
deprecations are real.

And the deeper honesty: gateway `/models` responses may **paginate**.
The sweep walks the pagination it can follow (`next_url`/`next` links,
`has_more` + `next_cursor` cursors). When enumeration cannot be
completed, **removals are withheld**: the line says `enumeration
INCOMPLETE`, additions are still proposed (they were enumerated), and
the ids that merely *weren't seen* are left alone - an id that was
never enumerated is **unknown, not deprecated**. One more floor under
every sweep: the model your config currently *runs on* is never
removed, even if the gateway stopped enumerating it - breaking a live
selection over a catalog technicality is not a refresh, it is a
brick. The protection says so; change the model selection first if
you truly want it gone.

## 4. Where keys come from - and why you never see them

`modelupdate` resolves a credential the way the harness itself resolves
it at launch, and reports only the **source label**:

| the config says | the tool does | you see |
|---|---|---|
| `{env:VAR}` | reads the variable | `key: env VAR` |
| `{file:PATH}` / `file:PATH` / `file://PATH` | reads the file (`~` expanded) | `key: file PATH` |
| an inline value | uses it | `key: inline (not shown)` |
| opencode auth store | consults it | `key: opencode auth store` |
| claude `apiKeyHelper: "…"` | **executes it** - the same command claude runs on every launch; env keys still take precedence, timeout 15 s | `key: apiKeyHelper (executed)` |

The values never surface: not in labels, not in diffs, not in errors -
a helper's failing output (even on stderr) is reported as an **exit
code only**. A reference that resolves to nothing is called out as
broken and the provider is **not probed**: the raw reference string is
never sent as a bearer token, and there is no silent fallback to
another source.

```sh
harnessator provider modelupdate claude --dry-run
```

```console
claude/gateway: anthropic-compatible https://api.example-gateway.com (key: apiKeyHelper (executed))
  report only (151 models reachable): claude has no model catalog in its config - tiers select at runtime; consider a profile via provider import + tune
```

Report-only is a property of the harness, not a failure: claude selects
by tier alias and has no catalog to write; codex's `config.toml`
carries hand comments a rewrite would eat; native surfaces (gemini) and
moving schemas (cline) are deliberately not rewritten in v1. The full
matrix:

| harness | modelupdate writes? | why |
|---|---|---|
| opencode | yes, with backup | JSONC catalog per provider |
| claude | report only | tiers select at runtime |
| codex | report only | hand-commented TOML stays untouched |
| cline, gemini | report only (skip) | moving schema / native API |
| copilot, cursor, antigravity, pi | nothing local | server-side or unmapped |
| harnessator-managed artifacts (`​.opencode/opencode.json` and anything `setup` renders) | **refused** | their truth lives in a profile source - fix it there and run `setup` (or `tune`) |

## 5. When fresh is not enough: make it governable

`modelupdate` keeps your harnesses honest **today**. When the same
truth must become pinned, reviewable and lock-visible, run the other
direction over it:

```sh
harnessator provider import "*" --dry-run   # bootstrap from what already works
```

`import` reads the live configs and writes a consumable source (env-var
*NAMES* cross the boundary; secret values never do; a `{file:}`
reference is reported by path with the ramp to make it governable).
Then `source add` → `provider use` → `harnessator tune --yes` - and the
catalog lives in git with the rest of the governance. Both orders are
first-class: work first, govern second, or govern first and let every
config fall out of it.

## 6. Dead ends are ramps

- `check the VALUE of VAR` - the gateway rejected the key: right
  gateway, wrong value for that variable.
- `the apiKey reference is configured but resolves to nothing` - unset
  variable or unreadable file; nothing was probed or written.
- `MASS REMOVAL REFUSED` - see §3; inspect before you opt in.
- `is a harnessator-managed artifact` - the file belongs to `setup`;
  edit the profile source, not the rendering.
- exit code 1 - at least one provider errored (an unreachable gateway,
  a broken write); report-only lines and refusals do not set it.

## What this does not do

`modelupdate` never touches governed profiles or the lock; `import`
never executes anything; policies still decide what each harness may
offer, whoever refreshed the catalog. Governance machinery behind this
page: [model-governance](../platform/model-governance.md) (the other
side of the table), [configure-gateway-access](configure-gateway-access.md)
(the platform-published side), [pointer-sources](../platform/pointer-sources.md)
(where the profile could live).
