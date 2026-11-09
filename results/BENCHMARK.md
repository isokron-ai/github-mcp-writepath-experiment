# Before/after benchmark: GitHub MCP server v2.1.0 vs the write-path experiment

Source: [IsoKron, GitHub MCP server audit (v2.1.0)](https://isokron.ai/research/github-mcp-server-audit). "Experimental build" below is this repository's `main` branch.


An IsoKron experimental build of v2.1.0 with a combined set of write-path fixes and usability changes cut MCP tool calls by 26% and turns by 18% per round of four jobs, and failed calls from 3 to 1, with all 24 runs passing. Results measure the combined set, not each change individually. For why we count calls and turns rather than tokens, see [why MCP tool design matters](https://isokron.ai/research/why-mcp-tool-design-matters).

Method: the four write-phase jobs, run 3 times each on v2.1.0 and 3 times each on the experimental build, by Claude Opus (claude-opus-5) in Claude Code 2.1.241 with MCP tools only. The harness was hashed and frozen before the first run, the repository was reset to the same state before every run, and only the server image changed between phases.

The experimental build adds write results that return the issue or PR number and URL; a one-call review with inline comments; consistent review-comment IDs; add-label and remove-label tools; per-field issue-field writes (the GH-01 fix); a whole-repo file list and multi-file read; create_branch from a SHA; a version check on delete_file and no folder deletes; push_files without silent branch creation or orphan branches; and an error on no-op updates.

Definitions used in the table:

- **Tool call:** one MCP tool invocation.
- **Turn:** one model response cycle.
- **Failed call:** an MCP call that returned an error.
- **Passing run:** the job's end state met its acceptance checks.

All 24 runs passed although some individual calls failed. Pass or fail is judged on the end state only, so a run passes when the agent works around a failed call, for example by resending a review that GitHub rejected as a comment.

Values are medians over 3 runs, with [min–max]. Bold marks non-overlapping observed ranges; with three runs per build this is descriptive, not a significance test. The per-round row sums one complete run of all four jobs and reports the median over the 3 rounds; it is not the sum of the per-job medians.

| Job | MCP tool calls, v2.1.0 → experimental | Turns, v2.1.0 → experimental | Failed calls, v2.1.0 → experimental |
| --- | --- | --- | --- |
| Bug fix → PR → merge | **30 [27–30] → 25 [21–25] (−17%)** | 37 [34–38] → 32 [32–34] | **1 [1–1] → 0 [0–0]** |
| PR review with 3 inline comments | 21 [19–22] → 17 [15–20] | 25 [25–27] → 24 [21–30] | 2 [2–2] → 1 [1–2] |
| Triage 10 issues | **35 [33–35] → 25 [25–28] (−29%)** | **43 [40–46] → 35 [34–36] (−19%)** | 0 → 0 |
| Rename across 3 files, one commit | **22 [22–25] → 14 [11–18] (−36%)** | **27 [27–32] → 21 [20–23] (−22%)** | 0 → 0 |
| Per round of all four jobs | **109 [101–111] → 81 [81–82] (−26%)** | **137 [126–138] → 113 [111–118] (−18%)** | **3 [3–3] → 1 [1–2]** |

- Where the calls went: the whole-repo file list and multi-file read replaced folder-by-folder crawls. Across the three runs of each job, get_file_contents calls fell from 40 to 8 in the rename job, 34 to 14 in the bug fix and 32 to 0 in triage.
- Reviews: the one-call review cut write tool calls in the review job from 11 to 6 per run (−45%); a review with 3 inline comments went from 5 calls to 1.
- Error rate: 9 of 321 tool calls failed on v2.1.0 (2.8%) and 4 of 244 on the experimental build (1.6%). On v2.1.0, the bug-fix runs hit a missing path while crawling, and all 3 review runs failed create_branch with a SHA. The remaining failures in both builds are GitHub rejecting a review that requests changes on the agent's own pull request; the experimental build returns that rejection at the create step instead of the submit step.
- Read-only baseline: in 27 read-only runs across tool profiles of 9, 27 and 57 tools, every run found the expected facts with 0 tool errors; 18 more runs with every tool definition loaded up front also had 0 tool errors.

### Catalog size

The experimental build's four extra tools add 984 tokens to the catalog, +9 to 10%. For reference, v2.1.0's default toolsets are about 11.2K model-visible tokens and all 91 tools about 23.4K; icons make up 56% of the full tools/list bytes. Token figures use cl100k as a common baseline; cl100k is not Claude's tokenizer, and the figures are estimates of definition size. How much of the catalog reaches the model depends on the client: in these runs Claude Code loaded definitions on demand through its tool search, while a client that loads every definition up front puts the whole catalog in context on every turn.

