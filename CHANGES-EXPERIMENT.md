# Changes in the write-path experiment (github-mcp-server v2.1.0)

Fork of `github/github-mcp-server` at tag `v2.1.0` (commit `4bcdcdf8`), branch
`main` of this repository. Each change targets one friction item
measured in the v2.1.0 audit runs (bug fix to PR to merge, PR review with line
comments, triage of 10 issues, 3-file rename in one commit). The commits are in
the order below; `chore: update toolsnaps and generated docs` at the end carries
the snapshot and README/docs updates for all of them.

Build it from the repository `Dockerfile` or with `go build ./cmd/github-mcp-server`; run it exactly like the official server.

## Item 5: issue field lost update (upstream issue 3751)

Commits `fix(issues): stop issue_write field updates reverting concurrent changes`
and `fix(issues): write issue field values before the issue PATCH`.

- **Why:** `issue_write update` read every field value, merged the requested
  change and sent the whole set in the issue PATCH, so two overlapping updates
  to different fields reverted each other.
- **Change:** field values are written only through the additive
  `POST …/issue-field-values` endpoint and the per-field `DELETE` (404 treated
  as already cleared), never in the PATCH. The writes happen before the PATCH so
  a rejected value fails the call before anything else changes; a PATCH failure
  after field writes says so. Adapted from the reference patch (made against
  `main`); it applied to v2.1.0 without conflicts.
- **Files:** `pkg/github/issues.go`, `pkg/github/helper_test.go`,
  `pkg/github/issues_delete_test.go`, `pkg/github/issues_test.go`,
  `pkg/github/typed_consolidated_issue_outputs_test.go`.
- **Tests:** `Test_UpdateIssue_SetFieldDoesNotRewriteOtherFields` (a stale
  snapshot of another field is never written back),
  `Test_UpdateIssue_DeleteFieldOnlyClearsThatField`,
  `Test_UpdateIssue_DeleteAbsentFieldIsNoOp`,
  `Test_UpdateIssue_DeleteContinuesOnPartialFailure`,
  `Test_UpdateIssue_FieldWriteFailuresReportWhatWasApplied`, plus the typed
  issue_write cases.

## Items 7 and 8: create_branch from a SHA; write safety

Commit `feat(repos): create_branch from_sha; safer delete_file and push_files; reject no-op issue updates`.

- **create_branch `from_sha` (item 7):** optional full 40-character commit SHA
  to branch from. Rejected when combined with `from_branch` or when not a full
  SHA. No ref lookup is made when it is given.
- **delete_file (item 8):** looks the path up in the branch's tree first and
  refuses a directory (a tree entry with a null SHA would delete the whole
  directory) or a missing file. New optional `sha` precondition: the delete
  fails when the file's blob SHA differs.
- **push_files (item 8):** a missing branch is an error unless the new
  `create_branch: true` is given. When creating, the branch ref is created only
  after the commit exists (`POST git/refs` at the new commit), so a failure part
  way leaves no orphan branch; the default branch is never moved. Empty
  repositories keep working, with the same `create_branch` rule for a
  non-default branch. The now-unused `createReferenceFromDefaultBranch` helper
  was removed.
- **issue_write update (item 8):** an update that would change nothing (no
  title, body, labels, assignees, milestone, type, state, state_reason or
  issue_fields, for example only unknown parameters) returns an error instead
  of an empty PATCH reported as success.
- **Files:** `pkg/github/repositories.go`, `pkg/github/repositories_helper.go`,
  `pkg/github/repository_tool_types.go`, `pkg/github/issues.go`, tests in
  `pkg/github/repositories_test.go`, `pkg/github/issues_test.go`,
  `pkg/github/typed_repository_outputs_test.go`,
  `pkg/github/typed_consolidated_issue_outputs_test.go`.
- **Tests:** `Test_CreateBranch` (from_sha success with request-body check,
  combination and format errors); `Test_DeleteFile` (tree lookup added) and
  `Test_DeleteFile_Preconditions` (directory, missing file, SHA mismatch, with
  no write endpoints mocked); `Test_PushFiles` (missing branch without
  `create_branch`, default-branch resolution failure) and
  `Test_PushFiles_CreateBranch` (branch created at the new commit, default
  branch not moved; no branch created when the commit fails);
  `Test_UpdateIssueRejectsUpdateWithoutChanges` and the typed no-op cases.

## Item 4: add / remove labels without replacing the set

Commit `feat(issues): add add_issue_labels and remove_issue_labels`.

- **Why:** agents read the labels and wrote back the full set with
  `issue_write`, which loses concurrent changes.
- **Change:** two new tools in the default `issues` toolset.
  `add_issue_labels` uses `POST …/issues/{n}/labels` (append; readOnly false,
  destructive false, idempotent true). `remove_issue_labels` uses
  `DELETE …/labels/{name}` per label with the name path-escaped (destructive
  true, idempotent true); a 404 means the label was not on the issue and is
  reported in `not_present`; if nothing was removed it reads the labels, so a
  missing issue is still an error. Both return
  `{"number":N,"labels":[…]}` (the labels after the change).
- **Files:** `pkg/github/issue_labels.go` (new), `pkg/github/tools.go`,
  `pkg/github/helper_test.go`, `pkg/github/issue_labels_test.go` (new).
- **Tests:** add (request body, full returned set, empty name, API error);
  remove (only requested labels deleted, escaped names, not_present, read-back
  when none removed, missing issue is an error, other errors stop the call);
  toolsnaps and annotations.

## Items 1 and 3: numbers in write results; review comment IDs

Commit `feat: report number and html_url from write tools; expose review comment IDs`.

- **Item 1:** create/update results for issues, pull requests and comments now
  include `number` and `html_url` next to the existing `id`/`url`:
  `issue_write` create (REST and the GraphQL create-with-parent path, which now
  selects `number`) and update, `create_pull_request`, `update_pull_request`,
  `add_issue_comment`, `update_issue_comment` (number parsed from the comment's
  issue URL), `add_reply_to_pull_request_comment`, and the feature-flagged
  granular issue/PR update tools. For a comment, `number` is its issue or pull
  request. Structured outputs (`IssueCommentOutput`,
  `PullRequestMutationReference`, `MinimalResponse`) gained the same optional
  fields. Because `MinimalResponse` is shared, the output schemas of gist,
  repository, fork and reaction tools also list the two optional fields; those
  tools do not set them.
- **Item 3:** `pull_request_read get_review_comments` returns each comment's
  numeric `id` (GraphQL `fullDatabaseId`; `databaseId` no longer exists on
  `PullRequestReviewComment`), which is what
  `add_reply_to_pull_request_comment` takes as `commentId`. The `commentId`
  description now points to it.
- **Files:** `pkg/github/minimal_types.go`, `pkg/github/issues.go`,
  `pkg/github/issues_granular.go`, `pkg/github/pullrequests.go`,
  `pkg/github/pullrequests_granular.go`, `pkg/github/issue_tool_types.go`,
  `pkg/github/consolidated_pull_request_types.go`, tests in
  `pkg/github/pullrequests_test.go` and the `typed_*_outputs_test.go` files.
- **Tests:** every pinned legacy-text and structured output for these tools was
  updated to the new shape (create/update issue via REST and GraphQL,
  create/update PR, comments, PR replies, granular tools); review-thread test
  asserts the numeric `id` and that an unknown id is omitted, not reported as 0.

## Item 2: one-call review with inline comments

Commit `feat(pull_requests): create a review with inline comments in one call`.

- **Why:** a review with line comments took 5 calls (create pending, add each
  comment, submit).
- **Change:** `pull_request_review_write` method `create` accepts
  `comments: [{path, line, side?, start_line?, start_side?, body}]`. They are
  sent as `threads` of the same `addPullRequestReview` mutation, so with
  `event` (COMMENT / APPROVE / REQUEST_CHANGES) the body, event and inline
  comments take one call; without `event` they go into the pending review.
  `side` defaults to RIGHT, `start_side` to `side`; `start_line` must not be
  after `line`; `comments` with any other method is an error. The success text
  adds "with N inline comments". Kept on the existing tool (no new tool name).
- **Files:** `pkg/github/pullrequests.go`,
  `pkg/github/consolidated_pull_request_types.go`,
  `pkg/github/pullrequests_test.go`.
- **Tests:** `TestCreatePullRequestReviewWithInlineComments` (exact mutation
  input with two threads incl. a LEFT multi-line range; rejection on
  `submit_pending`; invalid range); existing review tests unchanged.

## Item 6: whole-repo file list and multi-file read

Commit `feat(repos): add list_repository_files and get_files`.

- **Why:** without a tree or multi-file read in the default toolsets, agents
  crawled directories (12 to 13 calls).
- **Change:** two new read-only tools in the default `repos` toolset.
  `list_repository_files` (owner, repo, ref?, path?, max_entries?) returns
  `{ref, path?, total, truncated, files:[{path,size}]}` from one recursive tree
  call: files only, directory-aligned path filter, default 1000 and at most
  5000 entries, `truncated` also set when GitHub truncates the tree.
  `get_files` (owner, repo, paths[1..25], ref?, max_bytes?) returns
  `{files:[{path,sha,size,content,truncated?|error}], bytes, truncated}` in
  request order. Problems with one file (not found, directory, binary,
  symlink or submodule, over 1 MB) are reported per file; other API errors
  fail the call. Content stops at `max_bytes` in total (default 200000, at most
  1000000, cut on a UTF-8 boundary) and files past the cap are not fetched.
  Content goes through the same blob-SHA verification as `get_file_contents`.
  The git toolset's `get_repository_tree` is unchanged.
- **Files:** `pkg/github/repository_files.go` (new), `pkg/github/tools.go`,
  `pkg/github/repository_files_test.go` (new).
- **Tests:** tree listing (default-branch resolution and exact tree URL,
  blob-only, prefix filter that does not match `srcgen/`, cap and truncation,
  path traversal rejected); get_files (order, per-file errors, binary
  detection, SHA, byte cap with no fetch past it, API error fails the call,
  path-count limit); `truncateUTF8`.

## Verification

- `go test -race ./...`: all 30 test packages pass (`pkg/github` 169 s).
- `golangci-lint` v2.14.0 (the version pinned in `script/lint`, installed with
  `go install`): 0 issues. `gofmt -s -l .`: clean.
- `script/generate-docs`: README and docs regenerated and committed; a re-run
  leaves no diff.
- Toolsnaps regenerated with `UPDATE_TOOLSNAPS=true` and committed.
  `v2.1.0-isokron-bench`, commit `f60009ab`).
- Build it from the repository `Dockerfile` or with `go build ./cmd/github-mcp-server`; run it exactly like the official server.

## Tool-list size (default toolsets, model-visible)

Same method as the audit: per tool, the JSON of `name` + `description` +
`inputSchema` (compact, sorted keys), summed; tokens with tiktoken
`cl100k_base` (reference scale, not a Claude tokenizer). Both servers run as
images over stdio with the same token (`tools/measure.py`, raw data in
`toolsize.json`).

| server | tools | bytes | cl100k tokens |
| --- | ---: | ---: | ---: |
| v2.1.0 (`ghcr.io/github/github-mcp-server@sha256:f6056fc1…0f8a`) | 46 | 49,361 | 10,732 |
| fork (`isokron/github-mcp-server:bench`) | 50 | 53,856 | 11,716 |

Delta: +4 tools, +4,495 bytes, +984 tokens (+9%). Changed existing tools:
`pull_request_review_write` +971 bytes (comments schema), `push_files` +175,
`delete_file` +153, `create_branch` +136, `add_reply_to_pull_request_comment`
+60. Output schemas are not counted (they grew too, see items 1 and 4).

## Live smoke test

Against `a private test repository` with the write token loaded by
path (`tools/smoke.py`, wire log with the token redacted in
`smoke_wire.log`): 28 of 28 checks pass. Covered: `list_repository_files`
(whole repo and a directory), `get_files` (with a missing path and a byte
cap), `create_branch from_sha`, `push_files` (missing branch refused; existing
branch; `create_branch: true`), `delete_file` (directory refused, SHA mismatch
refused, delete with the right SHA), `create_pull_request` / `update_pull_request`
(number and html_url), one-call `pull_request_review_write` with two inline
comments, `get_review_comments` ids (values above 2^31) used directly by
`add_reply_to_pull_request_comment`, `issue_write` create / no-op update
refused / update, `add_issue_comment`, `update_issue_comment`,
`add_issue_labels` (twice, appending), `remove_issue_labels` (with an absent
label). Item 5 (`tools/f5.py`, `smoke_f5_wire.log`): set Priority, then set
Effort in a separate call: both values present; clearing Priority leaves
Effort.

Writes: 20 successful write tool calls (17 smoke + 3 issue-field), plus the
three `p3f-label-*` labels GitHub created on first use. Objects created:
branches `p3f-205536-a` and `p3f-205536-b`, PR 113 and issue 114 (both closed
at the end), all titled or prefixed `p3f`.

## Changes that alter names or semantics (for the benchmark prompts)

- New tools in the default toolsets: `add_issue_labels`,
  `remove_issue_labels` (issues), `list_repository_files`, `get_files`
  (repos). No tool was renamed or removed.
- `push_files` no longer creates a missing branch on its own: it fails unless
  `create_branch: true` is passed. A prompt that relied on the old implicit
  creation now takes one failed call or an explicit flag.
- `delete_file` refuses directories and missing files.
- `issue_write` update with nothing to change is an error.
- Additive only: `number`/`html_url` in write results, `id` in review
  comments, optional `comments` on `pull_request_review_write` (its success
  text adds "with N inline comments"), optional `from_sha` on `create_branch`,
  optional `sha` on `delete_file`.
