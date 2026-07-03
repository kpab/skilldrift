# skilldrift

English | [日本語](README.ja.md)

Dependabot for Claude Code Skills — a CLI + GitHub Action that watches installed skills for upstream changes, detects drift, and files Issues with a re-evaluated risk assessment.

Existing skill scanners stop at a one-shot "pre-install scan". skilldrift watches what happens **after** install: when a skill you trusted and adopted changes its contents through an update, skilldrift makes sure you notice.

## Usage

### CLI

```sh
# Record each skill's provenance into skilldrift.lock (fill in source.repo / commit by hand)
skilldrift init

# Detect diffs against upstream (exit codes: 0=no drift / 1=drift detected / 2=error)
skilldrift check

# Also create/update a GitHub Issue when drift is detected
GITHUB_TOKEN=... skilldrift check -issue -issue-repo owner/repo
```

For any skill where drift is detected, if [SkillSpector](https://github.com/NVIDIA/SkillSpector) is installed, skilldrift re-scans both your current local version and the new upstream version, compares the old and new risk scores, and reports the result to the terminal and the Issue (it is skipped automatically when SkillSpector is not installed). Disable it with `-scan=false`.

### GitHub Action

Commit `skilldrift.lock` to the repository that manages your skills, then add a workflow:

```yaml
name: skilldrift
on:
  schedule:
    - cron: "0 0 * * *" # 09:00 JST daily
  workflow_dispatch:

permissions:
  contents: read
  issues: write

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: kpab/skilldrift@main
```

When drift is detected, one Issue is filed per skill. While an open Issue with the same content exists, no duplicate is filed; if upstream advances further, the existing Issue's body is updated and a comment notifies you.

By default the action installs SkillSpector via `uv` and includes an old-vs-new risk score comparison in the Issue. To avoid the setup and runtime cost, disable it with `with: { scan-risk: "false" }`.

## Status

M3 complete (risk re-evaluation, distribution via goreleaser, v0.1.0 released). See docs/REQUIREMENTS.md for the plan.

## License

MIT
