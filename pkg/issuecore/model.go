package issuecore

import (
	"encoding/json"
	"time"
)

type IssueState string

const (
	IssueStateOpen   IssueState = "open"
	IssueStateClosed IssueState = "closed"
)

type IssueStateFilter string

const (
	IssueStateFilterOpen   IssueStateFilter = "open"
	IssueStateFilterClosed IssueStateFilter = "closed"
	IssueStateFilterAll    IssueStateFilter = "all"
)

type IssueStateReason string

const (
	IssueStateReasonCompleted  IssueStateReason = "completed"
	IssueStateReasonDuplicate  IssueStateReason = "duplicate"
	IssueStateReasonNotPlanned IssueStateReason = "not_planned"
	IssueStateReasonReopened   IssueStateReason = "reopened"
)

type PullRequestState string

const (
	PullRequestStateOpen   PullRequestState = "open"
	PullRequestStateClosed PullRequestState = "closed"
	PullRequestStateMerged PullRequestState = "merged"
)

type Actor struct {
	ID      string `json:"id,omitempty"`
	NodeID  string `json:"node_id,omitempty"`
	Login   string `json:"login,omitempty"`
	Type    string `json:"type,omitempty"`
	URL     string `json:"url,omitempty"`
	HTMLURL string `json:"html_url,omitempty"`
}

type Label struct {
	ID          string `json:"id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"default,omitempty"`
}

type Milestone struct {
	ID          string     `json:"id,omitempty"`
	NodeID      string     `json:"node_id,omitempty"`
	Number      int        `json:"number,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	State       IssueState `json:"state,omitempty"`
	DueOn       *time.Time `json:"due_on,omitempty"`
}

type ReactionRollup struct {
	URL        string `json:"url,omitempty"`
	TotalCount int    `json:"total_count,omitempty"`
	PlusOne    int    `json:"+1,omitempty"`
	MinusOne   int    `json:"-1,omitempty"`
	Laugh      int    `json:"laugh,omitempty"`
	Confused   int    `json:"confused,omitempty"`
	Heart      int    `json:"heart,omitempty"`
	Hooray     int    `json:"hooray,omitempty"`
	Rocket     int    `json:"rocket,omitempty"`
	Eyes       int    `json:"eyes,omitempty"`
}

type Comment struct {
	ID                string          `json:"id,omitempty"`
	NodeID            string          `json:"node_id,omitempty"`
	URL               string          `json:"url,omitempty"`
	HTMLURL           string          `json:"html_url,omitempty"`
	Body              string          `json:"body,omitempty"`
	BodyText          string          `json:"body_text,omitempty"`
	User              *Actor          `json:"user,omitempty"`
	AuthorAssociation string          `json:"author_association,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	Reactions         *ReactionRollup `json:"reactions,omitempty"`
	Pinned            bool            `json:"pinned,omitempty"`
	PinnedAt          *time.Time      `json:"pinned_at,omitempty"`
	PinnedBy          *Actor          `json:"pinned_by,omitempty"`
	MinimizedReason   string          `json:"minimized_reason,omitempty"`
	ProviderRaw       json.RawMessage `json:"provider_raw,omitempty"`
}

type TimelineEvent struct {
	ID          string          `json:"id,omitempty"`
	NodeID      string          `json:"node_id,omitempty"`
	Kind        string          `json:"kind"`
	Actor       *Actor          `json:"actor,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	ProviderRaw json.RawMessage `json:"provider_raw,omitempty"`
}

type PullRequestRef struct {
	Number             int              `json:"number,omitempty"`
	ID                 string           `json:"id,omitempty"`
	NodeID             string           `json:"node_id,omitempty"`
	Repository         string           `json:"repository,omitempty"`
	URL                string           `json:"url,omitempty"`
	HTMLURL            string           `json:"html_url,omitempty"`
	DiffURL            string           `json:"diff_url,omitempty"`
	PatchURL           string           `json:"patch_url,omitempty"`
	HeadRefName        string           `json:"head_ref_name,omitempty"`
	BaseRefName        string           `json:"base_ref_name,omitempty"`
	Draft              bool             `json:"draft,omitempty"`
	State              PullRequestState `json:"state,omitempty"`
	MergedAt           *time.Time       `json:"merged_at,omitempty"`
	ReviewDecision     string           `json:"review_decision,omitempty"`
	Mergeable          *bool            `json:"mergeable,omitempty"`
	MergeStateStatus   string           `json:"merge_state_status,omitempty"`
	RequestedReviewers []Actor          `json:"requested_reviewers,omitempty"`
	ProviderRaw        json.RawMessage  `json:"provider_raw,omitempty"`
}

type Issue struct {
	Provider           string            `json:"provider"`
	Repository         string            `json:"repository,omitempty"`
	ID                 string            `json:"id,omitempty"`
	NodeID             string            `json:"node_id,omitempty"`
	Number             int               `json:"number,omitempty"`
	URL                string            `json:"url,omitempty"`
	HTMLURL            string            `json:"html_url,omitempty"`
	Title              string            `json:"title"`
	Body               string            `json:"body,omitempty"`
	BodyText           string            `json:"body_text,omitempty"`
	State              IssueState        `json:"state"`
	StateReason        IssueStateReason  `json:"state_reason,omitempty"`
	User               *Actor            `json:"user,omitempty"`
	AuthorAssociation  string            `json:"author_association,omitempty"`
	Labels             []Label           `json:"labels,omitempty"`
	Milestone          *Milestone        `json:"milestone,omitempty"`
	Assignee           *Actor            `json:"assignee,omitempty"`
	Assignees          []Actor           `json:"assignees,omitempty"`
	Comments           int               `json:"comments,omitempty"`
	CommentItems       []Comment         `json:"comment_items,omitempty"`
	Locked             bool              `json:"locked,omitempty"`
	ActiveLockReason   string            `json:"active_lock_reason,omitempty"`
	Reactions          *ReactionRollup   `json:"reactions,omitempty"`
	Timeline           []TimelineEvent   `json:"timeline,omitempty"`
	PullRequest        *PullRequestRef   `json:"pull_request,omitempty"`
	LinkedPullRequests []PullRequestRef  `json:"linked_pull_requests,omitempty"`
	Dispatch           *DispatchMetadata `json:"dispatch,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	ClosedAt           *time.Time        `json:"closed_at,omitempty"`
	ClosedBy           *Actor            `json:"closed_by,omitempty"`
	ProviderRaw        json.RawMessage   `json:"provider_raw,omitempty"`
}
