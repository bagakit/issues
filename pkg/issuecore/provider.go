package issuecore

import "context"

const (
	ProviderLocal         = "local"
	ProviderGitHub        = "github"
	ProviderStageActive   = "active"
	ProviderStageScaffold = "scaffold"
)

type Provider interface {
	Descriptor() ProviderDescriptor
	CreateIssue(ctx context.Context, input CreateIssueInput) (Issue, error)
	ListIssues(ctx context.Context, query ListIssuesQuery) (IssuePage, error)
	GetIssue(ctx context.Context, locator IssueLocator) (Issue, error)
	UpdateIssue(ctx context.Context, locator IssueLocator, patch IssuePatch) (Issue, error)
	AddComment(ctx context.Context, locator IssueLocator, input AddCommentInput) (Comment, error)
	CloseIssue(ctx context.Context, locator IssueLocator, input CloseIssueInput) (Issue, error)
	ReopenIssue(ctx context.Context, locator IssueLocator, input ReopenIssueInput) (Issue, error)
}

type ProviderDescriptor struct {
	Name         string        `json:"name"`
	Kind         string        `json:"kind"`
	Title        string        `json:"title,omitempty"`
	Stage        string        `json:"stage,omitempty"`
	Capabilities CapabilitySet `json:"capabilities"`
}

type CapabilitySet struct {
	Create  bool `json:"create"`
	List    bool `json:"list"`
	Get     bool `json:"get"`
	Update  bool `json:"update"`
	Comment bool `json:"comment"`
	Close   bool `json:"close"`
	Reopen  bool `json:"reopen"`
}

type IssueLocator struct {
	Provider   string `json:"provider,omitempty"`
	Repository string `json:"repository,omitempty"`
	ID         string `json:"id,omitempty"`
	Number     int    `json:"number,omitempty"`
}

type CreateIssueInput struct {
	Repository string   `json:"repository,omitempty"`
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Assignees  []string `json:"assignees,omitempty"`
	Milestone  string   `json:"milestone,omitempty"`
}

type ListIssuesQuery struct {
	Repository string           `json:"repository,omitempty"`
	State      IssueStateFilter `json:"state,omitempty"`
	Labels     []string         `json:"labels,omitempty"`
	Assignee   string           `json:"assignee,omitempty"`
	Search     string           `json:"search,omitempty"`
	Limit      int              `json:"limit,omitempty"`
	PageToken  string           `json:"page_token,omitempty"`
}

type IssuePage struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"next_page_token,omitempty"`
}

type IssuePatch struct {
	Title       *string           `json:"title,omitempty"`
	Body        *string           `json:"body,omitempty"`
	Labels      *[]string         `json:"labels,omitempty"`
	Assignees   *[]string         `json:"assignees,omitempty"`
	Milestone   *string           `json:"milestone,omitempty"`
	StateReason *IssueStateReason `json:"state_reason,omitempty"`
}

type AddCommentInput struct {
	Body string `json:"body"`
}

type CloseIssueInput struct {
	Reason IssueStateReason `json:"reason,omitempty"`
}

type ReopenIssueInput struct {
	Reason IssueStateReason `json:"reason,omitempty"`
}
