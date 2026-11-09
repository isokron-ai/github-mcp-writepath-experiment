package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	ghErrors "github.com/github/github-mcp-server/v2/pkg/errors"
	"github.com/github/github-mcp-server/v2/pkg/ifc"
	"github.com/github/github-mcp-server/v2/pkg/inventory"
	"github.com/github/github-mcp-server/v2/pkg/scopes"
	"github.com/github/github-mcp-server/v2/pkg/translations"
	"github.com/github/github-mcp-server/v2/pkg/utils"
	"github.com/google/go-github/v92/github"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultRepositoryFilesLimit = 1000
	maxRepositoryFilesLimit     = 5000
	maxGetFilesPaths            = 25
	defaultGetFilesMaxBytes     = 200000
	maxGetFilesMaxBytes         = 1000000
)

type ListRepositoryFilesInput struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Ref        string `json:"ref,omitempty"`
	Path       string `json:"path,omitempty"`
	MaxEntries int    `json:"max_entries,omitempty"`
}

type RepositoryFileEntry struct {
	Path string `json:"path"`
	Size int    `json:"size"`
}

// ListRepositoryFilesOutput lists the files of a repository, capped at max_entries.
type ListRepositoryFilesOutput struct {
	Ref       string                `json:"ref"`
	Path      string                `json:"path,omitempty"`
	Total     int                   `json:"total"`
	Truncated bool                  `json:"truncated"`
	Files     []RepositoryFileEntry `json:"files"`
}

// ListRepositoryFiles creates a tool that lists all files of a repository in one call.
func ListRepositoryFiles(t translations.TranslationHelperFunc) inventory.ServerTool {
	return NewTool[ListRepositoryFilesInput, *ListRepositoryFilesOutput](
		ToolsetMetadataRepos,
		mcp.Tool{
			Name:         "list_repository_files",
			OutputSchema: repositoryOutputSchema[ListRepositoryFilesOutput](),
			Description:  t("TOOL_LIST_REPOSITORY_FILES_DESCRIPTION", "List all files in a repository, including subdirectories, with their sizes in bytes. Use it instead of listing directories one by one. Narrow large repositories with path."),
			Annotations: &mcp.ToolAnnotations{
				Title:        t("TOOL_LIST_REPOSITORY_FILES_USER_TITLE", "List repository files"),
				ReadOnlyHint: true,
			},
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"owner": {
						Type:        "string",
						Description: "Repository owner",
					},
					"repo": {
						Type:        "string",
						Description: "Repository name",
					},
					"ref": {
						Type:        "string",
						Description: "Branch, tag or commit SHA (defaults to the default branch)",
					},
					"path": {
						Type:        "string",
						Description: "Only list files under this directory",
					},
					"max_entries": {
						Type:        "integer",
						Description: fmt.Sprintf("Maximum number of files to return (default %d, at most %d)", defaultRepositoryFilesLimit, maxRepositoryFilesLimit),
						Minimum:     new(1.0),
						Maximum:     new(float64(maxRepositoryFilesLimit)),
					},
				},
				Required: []string{"owner", "repo"},
			},
		},
		scopes.PublicRead(scopes.Repo),
		func(ctx context.Context, deps ToolDependencies, _ *mcp.CallToolRequest, input ListRepositoryFilesInput) (*mcp.CallToolResult, *ListRepositoryFilesOutput, error) {
			if input.Owner == "" {
				return utils.NewToolResultError("missing required parameter: owner"), nil, nil
			}
			if input.Repo == "" {
				return utils.NewToolResultError("missing required parameter: repo"), nil, nil
			}
			limit := input.MaxEntries
			if limit == 0 {
				limit = defaultRepositoryFilesLimit
			}
			if limit < 1 || limit > maxRepositoryFilesLimit {
				return utils.NewToolResultError(fmt.Sprintf("max_entries must be between 1 and %d", maxRepositoryFilesLimit)), nil, nil
			}
			dir := strings.Trim(input.Path, "/")
			if dir != "" {
				cleaned, err := validateRelativePath(dir)
				if err != nil {
					return utils.NewToolResultError(fmt.Sprintf("invalid path: %s", err)), nil, nil
				}
				dir = cleaned
			}

			client, err := deps.GetClient(ctx)
			if err != nil {
				return utils.NewToolResultErrorFromErr("failed to get GitHub client", err), nil, nil
			}

			ref := input.Ref
			if ref == "" {
				repository, resp, err := client.Repositories.Get(ctx, input.Owner, input.Repo)
				if err != nil {
					return ghErrors.NewGitHubAPIErrorResponse(ctx, "failed to get repository", resp, err), nil, nil
				}
				_ = resp.Body.Close()
				ref = repository.GetDefaultBranch()
			}

			tree, resp, err := client.Git.GetTree(ctx, input.Owner, input.Repo, escapeGitTreeish(ref), true)
			if err != nil {
				return ghErrors.NewGitHubAPIErrorResponse(ctx, "failed to get repository tree", resp, err), nil, nil
			}
			defer func() { _ = resp.Body.Close() }()

			output := &ListRepositoryFilesOutput{
				Ref:       ref,
				Path:      dir,
				Truncated: tree.GetTruncated(),
				Files:     []RepositoryFileEntry{},
			}
			for _, entry := range tree.Entries {
				if entry.GetType() != "blob" {
					continue
				}
				if dir != "" && !strings.HasPrefix(entry.GetPath(), dir+"/") {
					continue
				}
				output.Total++
				if len(output.Files) < limit {
					output.Files = append(output.Files, RepositoryFileEntry{Path: entry.GetPath(), Size: entry.GetSize()})
				}
			}
			if output.Total > len(output.Files) {
				output.Truncated = true
			}

			result := MarshalledTextResult(output)
			result = attachRepoVisibilityIFCLabel(ctx, deps, client, input.Owner, input.Repo, result, ifc.LabelCommitContents)
			return result, output, nil
		},
	)
}

type GetFilesInput struct {
	Owner    string   `json:"owner"`
	Repo     string   `json:"repo"`
	Paths    []string `json:"paths"`
	Ref      string   `json:"ref,omitempty"`
	MaxBytes int      `json:"max_bytes,omitempty"`
}

type RepositoryFileContent struct {
	Path      string `json:"path"`
	SHA       string `json:"sha,omitempty"`
	Size      int    `json:"size,omitempty"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GetFilesOutput holds the requested files in request order. Content stops at max_bytes in total.
type GetFilesOutput struct {
	Files     []RepositoryFileContent `json:"files"`
	Bytes     int                     `json:"bytes"`
	Truncated bool                    `json:"truncated"`
}

// GetFiles creates a tool that reads several text files in one call.
func GetFiles(t translations.TranslationHelperFunc) inventory.ServerTool {
	return NewTool[GetFilesInput, *GetFilesOutput](
		ToolsetMetadataRepos,
		mcp.Tool{
			Name:         "get_files",
			OutputSchema: repositoryOutputSchema[GetFilesOutput](),
			Description:  t("TOOL_GET_FILES_DESCRIPTION", fmt.Sprintf("Read up to %d text files from a repository in one call. Files come back in the order requested; a file that cannot be read gets an error instead of content, and content stops once max_bytes have been returned in total.", maxGetFilesPaths)),
			Annotations: &mcp.ToolAnnotations{
				Title:        t("TOOL_GET_FILES_USER_TITLE", "Get several files"),
				ReadOnlyHint: true,
			},
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"owner": {
						Type:        "string",
						Description: "Repository owner",
					},
					"repo": {
						Type:        "string",
						Description: "Repository name",
					},
					"paths": {
						Type:        "array",
						Description: "File paths relative to the repository root",
						MinItems:    new(1),
						MaxItems:    new(maxGetFilesPaths),
						Items:       &jsonschema.Schema{Type: "string"},
					},
					"ref": {
						Type:        "string",
						Description: "Branch, tag or commit SHA (defaults to the default branch)",
					},
					"max_bytes": {
						Type:        "integer",
						Description: fmt.Sprintf("Maximum total bytes of content to return (default %d, at most %d)", defaultGetFilesMaxBytes, maxGetFilesMaxBytes),
						Minimum:     new(1.0),
						Maximum:     new(float64(maxGetFilesMaxBytes)),
					},
				},
				Required: []string{"owner", "repo", "paths"},
			},
		},
		scopes.PublicRead(scopes.Repo),
		func(ctx context.Context, deps ToolDependencies, _ *mcp.CallToolRequest, input GetFilesInput) (*mcp.CallToolResult, *GetFilesOutput, error) {
			if input.Owner == "" {
				return utils.NewToolResultError("missing required parameter: owner"), nil, nil
			}
			if input.Repo == "" {
				return utils.NewToolResultError("missing required parameter: repo"), nil, nil
			}
			if len(input.Paths) == 0 || len(input.Paths) > maxGetFilesPaths {
				return utils.NewToolResultError(fmt.Sprintf("paths must contain between 1 and %d paths", maxGetFilesPaths)), nil, nil
			}
			budget := input.MaxBytes
			if budget == 0 {
				budget = defaultGetFilesMaxBytes
			}
			if budget < 1 || budget > maxGetFilesMaxBytes {
				return utils.NewToolResultError(fmt.Sprintf("max_bytes must be between 1 and %d", maxGetFilesMaxBytes)), nil, nil
			}

			client, err := deps.GetClient(ctx)
			if err != nil {
				return utils.NewToolResultErrorFromErr("failed to get GitHub client", err), nil, nil
			}

			output := &GetFilesOutput{Files: make([]RepositoryFileContent, 0, len(input.Paths))}
			for _, requested := range input.Paths {
				if output.Bytes >= budget {
					output.Truncated = true
					output.Files = append(output.Files, RepositoryFileContent{Path: requested, Error: "not read: max_bytes reached"})
					continue
				}
				file, err := readRepositoryTextFile(ctx, client, input.Owner, input.Repo, input.Ref, requested)
				if err != nil {
					if apiErr, ok := errors.AsType[*repositoryFileAPIError](err); ok {
						return ghErrors.NewGitHubAPIErrorResponse(ctx, fmt.Sprintf("failed to get %s", requested), apiErr.resp, apiErr.err), nil, nil
					}
					output.Files = append(output.Files, RepositoryFileContent{Path: requested, Error: err.Error()})
					continue
				}
				if remaining := budget - output.Bytes; len(file.Content) > remaining {
					file.Content = truncateUTF8(file.Content, remaining)
					file.Truncated = true
					output.Truncated = true
				}
				output.Bytes += len(file.Content)
				output.Files = append(output.Files, *file)
			}

			result := MarshalledTextResult(output)
			result = attachRepoVisibilityIFCLabel(ctx, deps, client, input.Owner, input.Repo, result, ifc.LabelGetFileContents)
			return result, output, nil
		},
	)
}

// repositoryFileAPIError is an API failure that should fail the whole call,
// as opposed to a problem with one requested file.
type repositoryFileAPIError struct {
	resp *github.Response
	err  error
}

func (e *repositoryFileAPIError) Error() string { return e.err.Error() }

// readRepositoryTextFile reads one text file. Problems specific to the file
// (missing, a directory, binary, too large) are returned as plain errors.
func readRepositoryTextFile(ctx context.Context, client *github.Client, owner, repo, ref, requested string) (*RepositoryFileContent, error) {
	path, err := validateRelativePath(requested)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	fileContent, dirContent, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("not found")
		}
		return nil, &repositoryFileAPIError{resp: resp, err: err}
	}
	if dirContent != nil || fileContent == nil {
		return nil, fmt.Errorf("is a directory")
	}

	read, inspectResp, err := inspectRepositoryFile(ctx, client, owner, repo, ref, path, fileContent)
	if err != nil {
		if inspectResp != nil {
			return nil, &repositoryFileAPIError{resp: inspectResp, err: err}
		}
		return nil, err
	}
	if read.Metadata != nil {
		return nil, fmt.Errorf("is a %s; use get_file_contents", read.Metadata.Type)
	}
	if !read.ContentAvailable {
		return nil, fmt.Errorf("is larger than 1 MB (%d bytes); use get_file_contents", fileContent.GetSize())
	}
	if !isTextFileContent(read.Content) {
		return nil, fmt.Errorf("is a binary file (%d bytes)", fileContent.GetSize())
	}
	return &RepositoryFileContent{
		Path:    path,
		SHA:     fileContent.GetSHA(),
		Size:    fileContent.GetSize(),
		Content: string(read.Content),
	}, nil
}

// isTextFileContent reports whether content is text, using the same content
// sniffing as get_file_contents.
func isTextFileContent(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	contentType := http.DetectContentType(content)
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		strings.HasSuffix(contentType, "+json") ||
		strings.HasSuffix(contentType, "+xml")
}

// truncateUTF8 cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
