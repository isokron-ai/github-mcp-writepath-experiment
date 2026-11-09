# GitHub MCP server write-path experiment

An independent research experiment by [IsoKron](https://isokron.ai). It is based on
[github/github-mcp-server](https://github.com/github/github-mcp-server) at tag `v2.1.0`
(commit `4bcdcdf8`), with 8 commits that change how the server handles writes.

> **Not affiliated with, sponsored by, or endorsed by GitHub.** This is not an official
> GitHub release and not a supported distribution. It was built to measure what fixing
> write-path friction changes for AI agents, and it is published so others can inspect
> the changes and reproduce the measurement. Use the official server for real work.
> If you run this build, use a dedicated test organization and a fine-grained,
> short-lived token limited to disposable repositories.

## What it changes

Each change targets friction or a defect measured in IsoKron's
[audit of GitHub's MCP server v2.1.0](https://isokron.ai/research/github-mcp-server-audit).
Details, files and tests for each: [CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md).

| Change | Related upstream report |
| --- | --- |
| `issue_write` writes issue-field values per field instead of rewriting the whole set (concurrent updates to different fields no longer revert each other) | [#3751](https://github.com/github/github-mcp-server/issues/3751) |
| `delete_file` takes an optional `sha` precondition and refuses folders and missing files | [#3757](https://github.com/github/github-mcp-server/issues/3757) |
| `push_files` no longer creates a missing branch on its own; with `create_branch: true` the branch is created only after the commit exists, so a failure leaves no orphan branch | [#3755](https://github.com/github/github-mcp-server/issues/3755), [#3473](https://github.com/github/github-mcp-server/issues/3473) |
| `issue_write` update with nothing to change returns an error instead of success | [#3759](https://github.com/github/github-mcp-server/issues/3759) |
| Write results include the issue or pull request `number` and `html_url` | |
| `add_issue_labels` and `remove_issue_labels` change labels without replacing the set | [#2724](https://github.com/github/github-mcp-server/issues/2724) |
| `pull_request_review_write` creates a review with inline comments in one call | |
| Review comments expose the numeric `id` that `add_reply_to_pull_request_comment` takes | |
| `create_branch` accepts `from_sha` | |
| `list_repository_files` (whole-repo file list) and `get_files` (multi-file read) | |

## What it measured

Four write-phase jobs (bug fix to merged PR, PR review with 3 inline comments, triage
of 10 issues, a rename across 3 files in one commit), 3 runs each on v2.1.0 and 3 on
this build: 24 runs by Claude Opus in Claude Code 2.1.241 with MCP tools only. The
harness was hashed and frozen before the first run, the repository was reset to the
same state before every run, and only the server image changed between builds.

| Per round of all four jobs (median of 3, [min–max]) | v2.1.0 | this build |
| --- | --- | --- |
| MCP tool calls | 109 [101–111] | 81 [81–82] |
| Turns | 137 [126–138] | 113 [111–118] |
| Failed tool calls | 3 [3–3] | 1 [1–2] |
| Runs passing their end-state checks | 12 of 12 | 12 of 12 |

With 3 runs per build, the ranges are descriptive, not a significance test. Tokens,
cost and wall time were not distinguishable at this sample size. The results measure
the combined set of changes, not each change on its own. The four extra tools add
984 cl100k tokens (+9%) to the default catalog. Per-job tables, method and caveats:
[results/BENCHMARK.md](results/BENCHMARK.md).

## Build and compare

```sh
git clone <this repository> && cd github-mcp-writepath-experiment
git checkout main             # v2.1.0 + the experiment commits
go build -o bin/github-mcp-server-experiment ./cmd/github-mcp-server
git checkout v2.1.0           # the unmodified upstream release
go build -o bin/github-mcp-server-v2.1.0 ./cmd/github-mcp-server
```

Or apply the patch series to a clean checkout of upstream `v2.1.0`:

```sh
git clone https://github.com/github/github-mcp-server && cd github-mcp-server
git checkout v2.1.0 && git am /path/to/patches/*.patch
```

`go test ./...` runs the upstream tests plus the tests added for each change. Run either
binary exactly like the official server (`github-mcp-server stdio`, token in
`GITHUB_PERSONAL_ACCESS_TOKEN`) and point your MCP client at it.

The benchmark harness itself is not in this repository yet: it is tied to a private
test repository and will be published separately in a general form. You can compare
the two builds today by running the same agent task against each one and counting
tool calls, turns and failed calls.

## Reporting

- **Your results differ from ours:** please open an issue here with the job, model,
  client version and counts. Reproductions that disagree are the most useful feedback.
- **A bug in one of the 8 experiment commits:** open an issue here.
- **Anything in the upstream server:** report it to
  [github/github-mcp-server](https://github.com/github/github-mcp-server), following its
  [security policy](https://github.com/github/github-mcp-server/security/policy) for
  vulnerabilities. This repository does not track upstream.

This build is not maintained against later upstream releases. Fixes that prove useful
are offered upstream as small, separate pull requests.

## License

MIT, as upstream. GitHub's copyright and permission notice is kept in [LICENSE](LICENSE).
The experiment commits are by IsoKron (Stormhaven Enterprises LLC) and are released
under the same MIT license; see [NOTICE](NOTICE).
