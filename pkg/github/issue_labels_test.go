package github

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/github/github-mcp-server/v2/internal/toolsnaps"
	"github.com/github/github-mcp-server/v2/pkg/translations"
	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_AddIssueLabels(t *testing.T) {
	serverTool := AddIssueLabels(translations.NullTranslationHelper)
	tool := serverTool.Tool
	require.NoError(t, toolsnaps.Test(tool.Name, tool))

	assert.Equal(t, "add_issue_labels", tool.Name)
	assert.False(t, tool.Annotations.ReadOnlyHint)
	assert.False(t, *tool.Annotations.DestructiveHint)
	assert.True(t, tool.Annotations.IdempotentHint)

	tests := []struct {
		name           string
		handlers       map[string]http.HandlerFunc
		args           map[string]any
		expectedText   string
		expectedErrMsg string
	}{
		{
			name: "adds labels and returns the full set",
			handlers: map[string]http.HandlerFunc{
				PostReposIssuesLabelsByOwnerByRepoByIssueNumber: expectRequestBody(t, []any{"bug", "triage"}).andThen(
					mockResponse(t, http.StatusOK, []*github.Label{{Name: "existing"}, {Name: "bug"}, {Name: "triage"}}),
				),
			},
			args:         map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{"bug", "triage"}},
			expectedText: `{"number":7,"labels":["existing","bug","triage"]}`,
		},
		{
			name:           "rejects an empty label name",
			handlers:       map[string]http.HandlerFunc{},
			args:           map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{""}},
			expectedErrMsg: "labels must not contain empty names",
		},
		{
			name: "surfaces API errors",
			handlers: map[string]http.HandlerFunc{
				PostReposIssuesLabelsByOwnerByRepoByIssueNumber: mockResponse(t, http.StatusNotFound, map[string]string{"message": "Not Found"}),
			},
			args:           map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{"bug"}},
			expectedErrMsg: "failed to add labels",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := mustNewGHClient(t, MockHTTPClientWithHandlers(tc.handlers))
			deps := BaseDeps{Client: client}
			request := createMCPRequest(tc.args)
			result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
			require.NoError(t, err)
			if tc.expectedErrMsg != "" {
				require.True(t, result.IsError)
				assert.Contains(t, getErrorResult(t, result).Text, tc.expectedErrMsg)
				return
			}
			require.False(t, result.IsError, getTextResult(t, result).Text)
			assert.JSONEq(t, tc.expectedText, getTextResult(t, result).Text)
		})
	}
}

func Test_RemoveIssueLabels(t *testing.T) {
	serverTool := RemoveIssueLabels(translations.NullTranslationHelper)
	tool := serverTool.Tool
	require.NoError(t, toolsnaps.Test(tool.Name, tool))

	assert.Equal(t, "remove_issue_labels", tool.Name)
	assert.False(t, tool.Annotations.ReadOnlyHint)
	assert.True(t, *tool.Annotations.DestructiveHint)
	assert.True(t, tool.Annotations.IdempotentHint)

	// deleteLabel removes "bug" and "needs info" from an issue that also has "keep".
	deleteLabel := func(t *testing.T, deleted *[]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*deleted = append(*deleted, r.URL.EscapedPath())
			switch r.URL.EscapedPath() {
			case "/repos/owner/repo/issues/7/labels/bug":
				mockResponse(t, http.StatusOK, []*github.Label{{Name: "needs info"}, {Name: "keep"}})(w, r)
			case "/repos/owner/repo/issues/7/labels/needs%20info":
				mockResponse(t, http.StatusOK, []*github.Label{{Name: "keep"}})(w, r)
			default:
				mockResponse(t, http.StatusNotFound, map[string]string{"message": "Label does not exist"})(w, r)
			}
		}
	}

	t.Run("removes only the requested labels", func(t *testing.T) {
		var deleted []string
		client := mustNewGHClient(t, MockHTTPClientWithHandlers(map[string]http.HandlerFunc{
			DeleteReposIssuesLabelsByOwnerByRepoByIssueNumberByName: deleteLabel(t, &deleted),
		}))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{"bug", "missing", "needs info"}})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.False(t, result.IsError, getTextResult(t, result).Text)
		assert.JSONEq(t, `{"number":7,"labels":["keep"],"not_present":["missing"]}`, getTextResult(t, result).Text)
		assert.Equal(t, []string{
			"/repos/owner/repo/issues/7/labels/bug",
			"/repos/owner/repo/issues/7/labels/missing",
			"/repos/owner/repo/issues/7/labels/needs%20info",
		}, deleted)
	})

	t.Run("reads the labels when none were on the issue", func(t *testing.T) {
		var deleted []string
		client := mustNewGHClient(t, MockHTTPClientWithHandlers(map[string]http.HandlerFunc{
			DeleteReposIssuesLabelsByOwnerByRepoByIssueNumberByName: deleteLabel(t, &deleted),
			GetReposIssuesLabelsByOwnerByRepoByIssueNumber:          mockResponse(t, http.StatusOK, []*github.Label{{Name: "keep"}}),
		}))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{"missing"}})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.False(t, result.IsError, getTextResult(t, result).Text)

		var output IssueLabelsChangeOutput
		require.NoError(t, json.Unmarshal([]byte(getTextResult(t, result).Text), &output))
		assert.Equal(t, IssueLabelsChangeOutput{Number: 7, Labels: []string{"keep"}, NotPresent: []string{"missing"}}, output)
	})

	t.Run("a missing issue is an error, not a no-op", func(t *testing.T) {
		var deleted []string
		client := mustNewGHClient(t, MockHTTPClientWithHandlers(map[string]http.HandlerFunc{
			DeleteReposIssuesLabelsByOwnerByRepoByIssueNumberByName: deleteLabel(t, &deleted),
			GetReposIssuesLabelsByOwnerByRepoByIssueNumber:          mockResponse(t, http.StatusNotFound, map[string]string{"message": "Not Found"}),
		}))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{"missing"}})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, getErrorResult(t, result).Text, "failed to list labels")
	})

	t.Run("stops on other API errors", func(t *testing.T) {
		client := mustNewGHClient(t, MockHTTPClientWithHandlers(map[string]http.HandlerFunc{
			DeleteReposIssuesLabelsByOwnerByRepoByIssueNumberByName: mockResponse(t, http.StatusForbidden, map[string]string{"message": "Forbidden"}),
		}))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{"owner": "owner", "repo": "repo", "issue_number": float64(7), "labels": []any{"bug"}})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, getErrorResult(t, result).Text, `failed to remove label "bug"`)
	})
}
