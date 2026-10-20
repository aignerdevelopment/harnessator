# User Journeys

Four people, four starting points, one tool. Each journey links the
guides that walk it end to end - and every command block in those guides
is executed on a clean machine by CI before release, so the paths below
are literally walkable.

## 1. The solo developer - clean slate

*"New repo, new agent setup, zero drama."*

You start a project and want your agents governed from the first commit.
Morning: **[quickstart](dev/quickstart-new-project.md)** - `init`,
`enable`, `setup`, commit the lock; ten minutes. When the work gets
real, **[day in the life](dev/day-in-the-life.md)** shows the Tuesday
loop: `update` as a reviewable lock diff, `dev` mode to edit a skill
live, `contribute` to give it back. If your team runs a model gateway,
**[gateway access](dev/configure-gateway-access.md)** connects you
without ever pasting a token into a repo.

You are this person if you think "dotfiles repo" is a personality trait.

## 2. The team lead - taming existing sprawl

*"Every repo has its own copies, they all drifted, nobody knows who
changed what."*

Start with **[adopt existing projects](dev/adopt-existing-projects.md)**:
`scan` finds the scattered skills and rules and shows divergence by
digest, `harvest` publishes one governed source repo from it, `--adopt`
turns the donor projects into consumers. The diverged copies that need
human judgment land in a residual report - the guided tail walks you
through them; the tool ratifies; nothing is destroyed by policy.
Then make it stick: a `harnessator.team.toml` beside the project,
`source add --layer team`, and **[CI and review](platform/ci-and-review.md)**
so drift fails the pipeline instead of the demo.

You are this person if you have ever diffed two `SKILL.md` files by hand.

## 3. The platform engineer - building the corporate registry

*"Twelve teams, six harnesses, one security review, zero servers to run."*

**[Corporate registry](platform/corporate-registry.md)** is the blueprint:
what a source repo is, the five scope-layer files and how they merge,
tag-range releases consumers can review, the rollout order that doesn't
hurt. Governance is the same machinery:
**[model governance](platform/model-governance.md)** runs a live demo of
a strict policy *blocking* a project - the enforcement story includes
its limits, spelled out in **[security review](platform/security-review.md)**.
Distribute `/etc/harnessator/config.toml` with your MDM; the allowlist
and lock-mode do the rest.

You are this person if "works on my machine" offends you professionally.

## 4. The vibe coder - trying everything

*"Claude in the morning, opencode after lunch, and something new on Friday."*

**[A day in the life (II): the tool-hopper](dev/day-in-the-life-tool-hopper.md)**
is written for you: widen `targets` and every harness gets its native
folder from the same pinned content, `--layer local` gives each project
its own task toolchain, and `harnessator mcp serve` lets whichever agent
you are testing inspect its own configuration. **[Switching
harnesses](dev/switch-harness.md)** covers the serious version; the
cookbook's toolchain recipes cover the fast ones. Leaving a tool behind
is one line in `targets` - the knowledge stays, the copies were always
the problem.

You are this person if your bookmarks folder has a "AI tools" section.

---

Not sure where you fit? `harnessator init --source
https://github.com/aignerdevelopment/agentictools.git .` takes longer to
type than to undo, and `harnessator layers` will always tell you what is
actually in effect.
