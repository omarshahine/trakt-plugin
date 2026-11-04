# Using a maintained fork without waiting for upstream

Upstream acceptance is not technically required to use these MIT-licensed
features. The contributor fork already contains the submitted integration branch
`feat/verified-restore-suite`, linked from [upstream PR #39](https://github.com/omarshahine/trakt-plugin/pull/39). Later discovery work
is local until separately pushed. Submission is not deployment.

## Simplest route: pinned local build

1. Select a reviewed commit and retain the currently working CLI/plugin artifacts
   and plugin settings for rollback. Keep test and personal account credentials
   separated; no implicit account switch is part of deploying code.
2. Build the CLI to a NEW versioned path (`go build -o <new-cli-path> .`), and
   build the matching plugin (`npm ci`, `npm run build` inside openclaw/).
3. Verify that exact CLI/plugin pair with synthetic and dedicated-test-account
   acceptance, including the installed loader path and platform behavior.
4. Install/link that reviewed plugin build, and set its existing `cliPath` option
   to the matching versioned binary. The documented repository mechanism is
   `openclaw plugins install -l ./openclaw`; confirm current installation behavior
   before deployment. Keep only one active registration for trakt-tools.
5. Verify plugin loading and real calls, retain rollback paths, then deliberately
   select the intended account in a separately authorized step. Never replace a
   working binary in place or mix an old CLI with new tool definitions.

These are deployment steps to prepare/review, not actions performed by this doc.
No current plugin/config, credentials, service or installed binary was changed.

## Durable independent releases

Maintain a fork branch, regularly merge upstream fixes, and run CI for every
release candidate. Publish CLI/plugin artifacts under fork-owned release/package
namespaces with matching version metadata and checksums. Configure fork-specific
build/publishing workflows first: inherited workflows target the original package
names and must not be used blindly for an independent release. Public publication
is optional; versioned private/local artifacts are enough to use the features.

Upstream PRs can remain contributions while the fork is the deployment source.
When upstream lands equivalent work, compare behavior, reconcile branches and
choose deliberately whether to return to upstream releases. There is no need to
wait for a maintainer to begin benefiting from a tested pinned build.
