package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	ghErrors "github.com/github/github-mcp-server/v2/pkg/errors"
	"github.com/github/github-mcp-server/v2/pkg/inventory"
	"github.com/github/github-mcp-server/v2/pkg/translations"
	"github.com/github/github-mcp-server/v2/pkg/utils"
	"github.com/google/go-github/v92/github"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type IssueLabelsChangeInput struct {
	Owner       string   `json:"owner"`
	Repo        string   `json:"repo"`
	IssueNumber int      `json:"issue_number"`
	Labels      []string `json:"labels"`
}

// IssueLabelsChangeOutput reports an issue's labels after labels were added or removed.
type IssueLabelsChangeOutput struct {
	Number     int      `json:"number"`
	Labels     []string `json:"labels"`
	NotPresent []string `json:"not_present,omitempty"`
}

func issueLabelsChangeSchema(labelsDescription string) *jsonschema.Schema {
	return &jsonschema.Schema{
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
			"issue_number": {
				Type:        "number",
				Description: "Issue or pull request number",
				Minimum:     new(1.0),
			},
			"labels": {
				Type:        "array",
				Description: labelsDescription,
				MinItems:    new(1),
				Items:       &jsonschema.Schema{Type: "string", MinLength: new(1)},
			},
		},
		Required: []string{"owner", "repo", "issue_number", "labels"},
	}
}

func validateIssueLabelsChange(input IssueLabelsChangeInput) error {
	if err := validateIssueCoordinate(input.Owner, input.Repo, input.IssueNumber); err != nil {
		return err
	}
	if input.IssueNumber < 1 {
		return fmt.Errorf("issue_number must be greater than 0")
	}
	if len(input.Labels) == 0 {
		return fmt.Errorf("labels must contain at least one label")
	}
	for _, label := range input.Labels {
		if label == "" {
			return fmt.Errorf("labels must not contain empty names")
		}
	}
	return nil
}

func issueLabelsChangeResult(number int, labels []*github.Label, notPresent []string) (*mcp.CallToolResult, *IssueLabelsChangeOutput, error) {
	output := &IssueLabelsChangeOutput{Number: number, Labels: make([]string, 0, len(labels)), NotPresent: notPresent}
	for _, label := range labels {
		output.Labels = append(output.Labels, label.GetName())
	}
	return MarshalledTextResult(output), output, nil
}

// AddIssueLabels creates a tool that adds labels to an issue without replacing its existing labels.
func AddIssueLabels(t translations.TranslationHelperFunc) inventory.ServerTool {
	return NewTool[IssueLabelsChangeInput, *IssueLabelsChangeOutput](
		ToolsetMetadataIssues,
		mcp.Tool{
			Name:        "add_issue_labels",
			Description: t("TOOL_ADD_ISSUE_LABELS_DESCRIPTION", "Add labels to an issue or pull request. Labels already on it are kept, so there is no need to read them first. Returns the labels after the change."),
			Annotations: &mcp.ToolAnnotations{
				Title:           t("TOOL_ADD_ISSUE_LABELS_USER_TITLE", "Add labels to issue or pull request"),
				ReadOnlyHint:    false,
				DestructiveHint: new(false),
				IdempotentHint:  true,
			},
			InputSchema: issueLabelsChangeSchema("Label names to add"),
		},
		publicRepositoryWriteScopeAccess(),
		func(ctx context.Context, deps ToolDependencies, _ *mcp.CallToolRequest, input IssueLabelsChangeInput) (*mcp.CallToolResult, *IssueLabelsChangeOutput, error) {
			if err := validateIssueLabelsChange(input); err != nil {
				return utils.NewToolResultError(err.Error()), nil, nil
			}
			client, err := deps.GetClient(ctx)
			if err != nil {
				return utils.NewToolResultErrorFromErr("failed to get GitHub client", err), nil, nil
			}

			labels, resp, err := client.Issues.AddLabelsToIssue(ctx, input.Owner, input.Repo, input.IssueNumber, input.Labels)
			if err != nil {
				return ghErrors.NewGitHubAPIErrorResponse(ctx, "failed to add labels", resp, err), nil, nil
			}
			defer func() { _ = resp.Body.Close() }()

			return issueLabelsChangeResult(input.IssueNumber, labels, nil)
		},
		normalizeIssueStrings([]string{"owner", "repo"}, nil),
		normalizeIssueIntegers([]string{"issue_number"}, nil),
	)
}

// RemoveIssueLabels creates a tool that removes labels from an issue and keeps its other labels.
func RemoveIssueLabels(t translations.TranslationHelperFunc) inventory.ServerTool {
	return NewTool[IssueLabelsChangeInput, *IssueLabelsChangeOutput](
		ToolsetMetadataIssues,
		mcp.Tool{
			Name:        "remove_issue_labels",
			Description: t("TOOL_REMOVE_ISSUE_LABELS_DESCRIPTION", "Remove labels from an issue or pull request. Its other labels are kept. Returns the labels after the change; requested labels that were not on it are listed in not_present."),
			Annotations: &mcp.ToolAnnotations{
				Title:           t("TOOL_REMOVE_ISSUE_LABELS_USER_TITLE", "Remove labels from issue or pull request"),
				ReadOnlyHint:    false,
				DestructiveHint: new(true),
				IdempotentHint:  true,
			},
			InputSchema: issueLabelsChangeSchema("Label names to remove"),
		},
		publicRepositoryWriteScopeAccess(),
		func(ctx context.Context, deps ToolDependencies, _ *mcp.CallToolRequest, input IssueLabelsChangeInput) (*mcp.CallToolResult, *IssueLabelsChangeOutput, error) {
			if err := validateIssueLabelsChange(input); err != nil {
				return utils.NewToolResultError(err.Error()), nil, nil
			}
			client, err := deps.GetClient(ctx)
			if err != nil {
				return utils.NewToolResultErrorFromErr("failed to get GitHub client", err), nil, nil
			}

			// Each DELETE answers with the labels left on the issue. A 404 means
			// the label was not on it, which is the state the caller asked for.
			var remaining []*github.Label
			var notPresent []string
			removedAny := false
			for _, label := range input.Labels {
				path := fmt.Sprintf("repos/%s/%s/issues/%d/labels/%s", input.Owner, input.Repo, input.IssueNumber, url.PathEscape(label))
				req, err := client.NewRequest(ctx, http.MethodDelete, path, nil)
				if err != nil {
					return utils.NewToolResultErrorFromErr("failed to create request", err), nil, nil
				}
				var labels []*github.Label
				resp, err := client.Do(req, &labels)
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				if err != nil {
					if resp != nil && resp.StatusCode == http.StatusNotFound {
						notPresent = append(notPresent, label)
						continue
					}
					return ghErrors.NewGitHubAPIErrorResponse(ctx, fmt.Sprintf("failed to remove label %q", label), resp, err), nil, nil
				}
				remaining, removedAny = labels, true
			}

			// Nothing was removed, so no DELETE reported the current labels. Reading
			// them also turns a missing issue into an error instead of a no-op.
			if !removedAny {
				labels, resp, err := client.Issues.ListLabelsByIssue(ctx, input.Owner, input.Repo, input.IssueNumber, &github.ListOptions{PerPage: 100})
				if err != nil {
					return ghErrors.NewGitHubAPIErrorResponse(ctx, "failed to list labels", resp, err), nil, nil
				}
				defer func() { _ = resp.Body.Close() }()
				remaining = labels
			}

			return issueLabelsChangeResult(input.IssueNumber, remaining, notPresent)
		},
		normalizeIssueStrings([]string{"owner", "repo"}, nil),
		normalizeIssueIntegers([]string{"issue_number"}, nil),
	)
}
