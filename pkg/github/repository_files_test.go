package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/github/github-mcp-server/v2/internal/toolsnaps"
	"github.com/github/github-mcp-server/v2/pkg/translations"
	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ListRepositoryFiles(t *testing.T) {
	serverTool := ListRepositoryFiles(translations.NullTranslationHelper)
	tool := serverTool.Tool
	require.NoError(t, toolsnaps.Test(tool.Name, tool))
	assert.True(t, tool.Annotations.ReadOnlyHint)
	assert.Equal(t, ToolsetMetadataRepos.ID, serverTool.Toolset.ID)

	// Raw JSON: go-github's TreeEntry marshaling drops size, which the API does return.
	tree := `{"sha":"tree-sha","truncated":false,"tree":[
		{"path":"README.md","type":"blob","size":10},
		{"path":"src","type":"tree"},
		{"path":"src/a.go","type":"blob","size":20},
		{"path":"src/b.go","type":"blob","size":30},
		{"path":"srcgen/c.go","type":"blob","size":40},
		{"path":"vendor/lib","type":"commit"}
	]}`
	handlers := func(t *testing.T, treePath *string) map[string]http.HandlerFunc {
		return map[string]http.HandlerFunc{
			GetReposByOwnerByRepo: mockResponse(t, http.StatusOK, &github.Repository{DefaultBranch: new("main")}),
			GetReposGitTreesByOwnerByRepoByTree: func(w http.ResponseWriter, r *http.Request) {
				*treePath = r.URL.Path + "?" + r.URL.RawQuery
				mockResponse(t, http.StatusOK, tree)(w, r)
			},
		}
	}

	tests := []struct {
		name             string
		args             map[string]any
		expectedTreePath string
		expected         string
		expectedErrMsg   string
	}{
		{
			name:             "lists every file of the default branch",
			args:             map[string]any{"owner": "owner", "repo": "repo"},
			expectedTreePath: "/repos/owner/repo/git/trees/main?recursive=1",
			expected:         `{"ref":"main","total":4,"truncated":false,"files":[{"path":"README.md","size":10},{"path":"src/a.go","size":20},{"path":"src/b.go","size":30},{"path":"srcgen/c.go","size":40}]}`,
		},
		{
			name:             "keeps files under a directory and caps the count",
			args:             map[string]any{"owner": "owner", "repo": "repo", "ref": "feature", "path": "/src/", "max_entries": 1},
			expectedTreePath: "/repos/owner/repo/git/trees/feature?recursive=1",
			expected:         `{"ref":"feature","path":"src","total":2,"truncated":true,"files":[{"path":"src/a.go","size":20}]}`,
		},
		{
			name:           "rejects a path that leaves the repository",
			args:           map[string]any{"owner": "owner", "repo": "repo", "path": "../x"},
			expectedErrMsg: "invalid path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var treePath string
			client := mustNewGHClient(t, MockHTTPClientWithHandlers(handlers(t, &treePath)))
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
			assert.Equal(t, tc.expectedTreePath, treePath)
			assert.JSONEq(t, tc.expected, getTextResult(t, result).Text)
		})
	}
}

func Test_GetFiles(t *testing.T) {
	serverTool := GetFiles(translations.NullTranslationHelper)
	tool := serverTool.Tool
	require.NoError(t, toolsnaps.Test(tool.Name, tool))
	assert.True(t, tool.Annotations.ReadOnlyHint)
	assert.Equal(t, ToolsetMetadataRepos.ID, serverTool.Toolset.ID)

	file := func(path, content string) *github.RepositoryContent {
		return &github.RepositoryContent{
			Type:     new("file"),
			Path:     new(path),
			Encoding: new("base64"),
			Content:  new(base64.StdEncoding.EncodeToString([]byte(content))),
			SHA:      new(gitBlobSHA([]byte(content))),
			Size:     new(len(content)),
		}
	}
	var requested []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
		requested = append(requested, path+"@"+r.URL.Query().Get("ref"))
		switch path {
		case "README.md":
			mockResponse(t, http.StatusOK, file("README.md", "# Title\n"))(w, r)
		case "src/main.go":
			mockResponse(t, http.StatusOK, file("src/main.go", "package main\n\nfunc main() {}\n"))(w, r)
		case "src":
			mockResponse(t, http.StatusOK, []*github.RepositoryContent{file("src/main.go", "package main\n")})(w, r)
		case "logo.png":
			mockResponse(t, http.StatusOK, file("logo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))(w, r)
		case "forbidden.txt":
			mockResponse(t, http.StatusForbidden, map[string]string{"message": "Resource not accessible"})(w, r)
		default:
			mockResponse(t, http.StatusNotFound, map[string]string{"message": "Not Found"})(w, r)
		}
	}

	t.Run("reads files in order and reports per-file problems", func(t *testing.T) {
		requested = nil
		client := mustNewGHClient(t, MockHTTPClientWithHandler(handler))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{
			"owner": "owner", "repo": "repo", "ref": "dev",
			"paths": []any{"src/main.go", "missing.md", "src", "logo.png", "README.md"},
		})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.False(t, result.IsError, getTextResult(t, result).Text)

		var output GetFilesOutput
		require.NoError(t, json.Unmarshal([]byte(getTextResult(t, result).Text), &output))
		require.Len(t, output.Files, 5)
		assert.Equal(t, "package main\n\nfunc main() {}\n", output.Files[0].Content)
		assert.Equal(t, gitBlobSHA([]byte("package main\n\nfunc main() {}\n")), output.Files[0].SHA)
		assert.Equal(t, RepositoryFileContent{Path: "missing.md", Error: "not found"}, output.Files[1])
		assert.Equal(t, RepositoryFileContent{Path: "src", Error: "is a directory"}, output.Files[2])
		assert.Equal(t, "logo.png", output.Files[3].Path)
		assert.Contains(t, output.Files[3].Error, "binary")
		assert.Empty(t, output.Files[3].Content)
		assert.Equal(t, "# Title\n", output.Files[4].Content)
		assert.Equal(t, len("package main\n\nfunc main() {}\n")+len("# Title\n"), output.Bytes)
		assert.False(t, output.Truncated)
		assert.Equal(t, []string{"src/main.go@dev", "missing.md@dev", "src@dev", "logo.png@dev", "README.md@dev"}, requested)
	})

	t.Run("stops returning content at max_bytes", func(t *testing.T) {
		requested = nil
		client := mustNewGHClient(t, MockHTTPClientWithHandler(handler))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{
			"owner": "owner", "repo": "repo", "max_bytes": 12,
			"paths": []any{"src/main.go", "README.md"},
		})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.False(t, result.IsError, getTextResult(t, result).Text)

		var output GetFilesOutput
		require.NoError(t, json.Unmarshal([]byte(getTextResult(t, result).Text), &output))
		require.Len(t, output.Files, 2)
		assert.Equal(t, "package main", output.Files[0].Content)
		assert.True(t, output.Files[0].Truncated)
		assert.Equal(t, RepositoryFileContent{Path: "README.md", Error: "not read: max_bytes reached"}, output.Files[1])
		assert.Equal(t, 12, output.Bytes)
		assert.True(t, output.Truncated)
		assert.Equal(t, []string{"src/main.go@"}, requested, "files past the cap are not fetched")
	})

	t.Run("fails the call on errors that are not about one file", func(t *testing.T) {
		client := mustNewGHClient(t, MockHTTPClientWithHandler(handler))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{
			"owner": "owner", "repo": "repo", "paths": []any{"README.md", "forbidden.txt"},
		})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, getErrorResult(t, result).Text, "failed to get forbidden.txt")
	})

	t.Run("limits the number of paths", func(t *testing.T) {
		paths := make([]any, maxGetFilesPaths+1)
		for i := range paths {
			paths[i] = "README.md"
		}
		client := mustNewGHClient(t, MockHTTPClientWithHandler(handler))
		deps := BaseDeps{Client: client}
		request := createMCPRequest(map[string]any{"owner": "owner", "repo": "repo", "paths": paths})
		result, err := serverTool.Handler(deps)(ContextWithDeps(context.Background(), deps), &request)
		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, getErrorResult(t, result).Text, "paths")
	})
}

func Test_TruncateUTF8(t *testing.T) {
	assert.Equal(t, "ab", truncateUTF8("abc", 2))
	assert.Equal(t, "abc", truncateUTF8("abc", 5))
	assert.Equal(t, "a", truncateUTF8("aé", 2), "a two-byte rune is not split")
	assert.Equal(t, "", truncateUTF8("é", 1))
}
