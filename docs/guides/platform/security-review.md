# Security review (the honest one)

What harnessator's model actually protects against, and - equally
important - what it does not. Write this down before your first
enterprise conversation, not after.

## The threat model

The supply chain has four joints, and harnessator puts a control at each:

| joint | risk | control | how real |
|---|---|---|---|
| publishing a source | unreviewed component reaches everyone | git review + protected branches on source repos; the manifest is code-reviewed like code | ordinary review discipline - not tooling magic |
| after approval | a merged component is silently edited, swapped or force-pushed upstream | commit pins + per-component sha256 digests in `harnessator.lock`; `verify`/`check` recompute and fail | **strong** - content-addressed, CI-enforceable |
| on-disk | someone edits the installed artifact, breaking the reviewed state | drift detection with a pointer to the *right* channel (dev + contribute) | **strong** |
| governance | the wrong model gets used, sub-agents escape policy | strict policies fail setup; renderings narrowed per harness; sub-agent pins | **client-side - see below** |

## What the pinning genuinely buys

- **Post-approval change detection.** The digest in the lock is computed
  over component content; an upstream edit produces lock-diff noise or
  verify failure, never silent adoption.
- **Reproducible installs.** Same lock ⇒ same bytes on every machine,
  which makes "works on my laptop" apply to agent config in the good
  direction.
- **Auditability without infrastructure.** The lock is a committed,
  human-readable provenance record: source URL, exact commit, digest
  per component. Forensics is `git log harnessator.lock`.
- **No server in the trust path.** There is no hosted registry to
  breach or rug-pull; the format is the contract (see the corpus as the
  runnable spec).

## What it does not buy

Say these out loud; they are the questions that matter.

- **It is not an execution sandbox.** Components are prompts, rules and
  MCP server *definitions*; a malicious or careless rule is still a
  prompt injection with your permissions. harnessator renders them -
  it does not police what they tell the agent. Treat skill content like
  code you grant a smart, connected tool: review it. Capability tags in
  manifests (`network`, `exec`, `fs`) exist to make that review
  skimmable, not to enforce it.
- **Hooks and MCP servers are real code / real endpoints.** The store
  is read-only, but hook scripts run in the harness's context and MCP
  servers get whatever the harness gives them. The source repo's review
  is your firewall here.
- **Governance is a client-side gate.** Policy violations fail
  *harnessator* commands; a user who points the harness at the gateway
  by hand, outside the managed config, bypasses it. The durable control
  lives in the gateway (keys, budgets, logging) and in endpoint auth
  policies - harnessator makes the compliant path the easy path and
  the deviation expensive, which is what client-side tooling honestly
  is. (For harnesses with server-side model control - Cursor
  Team/Enterprise - the org policy overrides; harnessator reports that
  division, it does not paper over it.)
- **Git access is the supply chain.** A source repo you add is as
  trusted as its maintainers. Layered allowlists and platform-owned
  corporate sources exist to make "who can publish what I run" a policy
  question, not an accident.
- **Secrets: the model is names-only, enforced by gates.** Configs
  carry env-var names; `harnessator verify`'s sibling checks and the
  docs gate keep committed content clean, but `.devenvironment`/secret
  manager hygiene remains your practice.

## The trust story in one paragraph

harnessator is package management, not containment: it makes agent
configuration *reviewable, pinned, drift-detectable and reproducible* -
the same properties git+digests give a binary supply chain - and pairs
them with declarative model governance whose enforcement is strongest
exactly where client-side tooling can be (setup gates, narrowed
renderings, CI checks). Residual trust sits where it always has: in
code review of shared sources and in the gateway that holds the keys.
