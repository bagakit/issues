package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bagakit/issues/pkg/issuecore"
)

const (
	EnvToken           = "ISSUES_GITHUB_TOKEN"
	EnvTokenGH         = "GH_TOKEN"
	EnvTokenGitHub     = "GITHUB_TOKEN"
	EnvAPIBaseURL      = "ISSUES_GITHUB_API_URL"
	defaultBaseURL     = "https://api.github.com/"
	defaultAPIVersion  = "2026-03-10"
	defaultUserAgent   = "bagakit-issues"
	defaultHTTPTimeout = 30 * time.Second
)

var (
	validIssueStates = map[issuecore.IssueStateFilter]bool{
		"":                               true,
		issuecore.IssueStateFilterOpen:   true,
		issuecore.IssueStateFilterClosed: true,
		issuecore.IssueStateFilterAll:    true,
	}
)

type Config struct {
	Token      string
	BaseURL    string
	HTTPClient *http.Client
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Provider struct {
	token      string
	baseURL    *url.URL
	client     httpDoer
	descriptor issuecore.ProviderDescriptor
}

type RequestPlan struct {
	Operation string            `json:"operation"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      json.RawMessage   `json:"body,omitempty"`
}

type wireActor struct {
	ID      json.RawMessage `json:"id"`
	NodeID  string          `json:"node_id"`
	Login   string          `json:"login"`
	Type    string          `json:"type"`
	URL     string          `json:"url"`
	HTMLURL string          `json:"html_url"`
}

type wireLabel struct {
	ID          json.RawMessage `json:"id"`
	NodeID      string          `json:"node_id"`
	Name        string          `json:"name"`
	Color       string          `json:"color"`
	Description string          `json:"description"`
	IsDefault   bool            `json:"default"`
}

type wireMilestone struct {
	ID          json.RawMessage      `json:"id"`
	NodeID      string               `json:"node_id"`
	Number      int                  `json:"number"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	State       issuecore.IssueState `json:"state"`
	DueOn       *time.Time           `json:"due_on"`
}

type wireReactionRollup struct {
	URL        string `json:"url"`
	TotalCount int    `json:"total_count"`
	PlusOne    int    `json:"+1"`
	MinusOne   int    `json:"-1"`
	Laugh      int    `json:"laugh"`
	Confused   int    `json:"confused"`
	Heart      int    `json:"heart"`
	Hooray     int    `json:"hooray"`
	Rocket     int    `json:"rocket"`
	Eyes       int    `json:"eyes"`
}

type wirePullRequest struct {
	URL      string     `json:"url"`
	HTMLURL  string     `json:"html_url"`
	DiffURL  string     `json:"diff_url"`
	PatchURL string     `json:"patch_url"`
	MergedAt *time.Time `json:"merged_at"`
}

type wireIssue struct {
	ID                json.RawMessage            `json:"id"`
	NodeID            string                     `json:"node_id"`
	RepositoryURL     string                     `json:"repository_url"`
	Number            int                        `json:"number"`
	URL               string                     `json:"url"`
	HTMLURL           string                     `json:"html_url"`
	Title             string                     `json:"title"`
	Body              string                     `json:"body"`
	BodyText          string                     `json:"body_text"`
	State             issuecore.IssueState       `json:"state"`
	StateReason       issuecore.IssueStateReason `json:"state_reason"`
	User              *wireActor                 `json:"user"`
	AuthorAssociation string                     `json:"author_association"`
	Labels            []wireLabel                `json:"labels"`
	Milestone         *wireMilestone             `json:"milestone"`
	Assignee          *wireActor                 `json:"assignee"`
	Assignees         []wireActor                `json:"assignees"`
	Comments          int                        `json:"comments"`
	Locked            bool                       `json:"locked"`
	ActiveLockReason  string                     `json:"active_lock_reason"`
	Reactions         *wireReactionRollup        `json:"reactions"`
	PullRequest       *wirePullRequest           `json:"pull_request"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	ClosedAt          *time.Time                 `json:"closed_at"`
	ClosedBy          *wireActor                 `json:"closed_by"`
}

type wireComment struct {
	ID                json.RawMessage     `json:"id"`
	NodeID            string              `json:"node_id"`
	URL               string              `json:"url"`
	HTMLURL           string              `json:"html_url"`
	Body              string              `json:"body"`
	BodyText          string              `json:"body_text"`
	User              *wireActor          `json:"user"`
	AuthorAssociation string              `json:"author_association"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
	Reactions         *wireReactionRollup `json:"reactions"`
	Pinned            bool                `json:"pinned"`
	PinnedAt          *time.Time          `json:"pinned_at"`
	PinnedBy          *wireActor          `json:"pinned_by"`
	MinimizedReason   string              `json:"minimized_reason"`
}

type timelineSource struct {
	Type  string     `json:"type"`
	Issue *wireIssue `json:"issue"`
}

type renamePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type timelineEventResponse struct {
	ID        json.RawMessage `json:"id"`
	NodeID    string          `json:"node_id"`
	URL       string          `json:"url"`
	Event     string          `json:"event"`
	Actor     *wireActor      `json:"actor"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
	CommitID  string          `json:"commit_id"`
	CommitURL string          `json:"commit_url"`
	Source    *timelineSource `json:"source,omitempty"`
	Rename    *renamePayload  `json:"rename,omitempty"`
}

type apiErrorPayload struct {
	Message          string          `json:"message"`
	DocumentationURL string          `json:"documentation_url"`
	Errors           json.RawMessage `json:"errors"`
}

type repositoryRef struct {
	Owner string
	Repo  string
}

func (r repositoryRef) String() string {
	return r.Owner + "/" + r.Repo
}

func New(cfg Config) (*Provider, error) {
	baseURL, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	return &Provider{
		token:   strings.TrimSpace(cfg.Token),
		baseURL: baseURL,
		client:  client,
		descriptor: issuecore.ProviderDescriptor{
			Name:  issuecore.ProviderGitHub,
			Kind:  "github-rest",
			Title: "GitHub REST issue provider",
			Stage: issuecore.ProviderStageActive,
			Capabilities: issuecore.CapabilitySet{
				Create:  true,
				List:    true,
				Get:     true,
				Update:  true,
				Comment: true,
				Close:   true,
				Reopen:  true,
			},
		},
	}, nil
}

func (p *Provider) Descriptor() issuecore.ProviderDescriptor {
	return p.descriptor
}

func (p *Provider) PlanListIssues(query issuecore.ListIssuesQuery) (RequestPlan, error) {
	repo, err := parseRepository(query.Repository)
	if err != nil {
		return RequestPlan{}, p.operationError("list", "invalid_argument", err)
	}
	if strings.TrimSpace(query.Search) != "" {
		return RequestPlan{}, p.operationError("list", "invalid_argument", fmt.Errorf("github provider does not support repository issue text search"))
	}
	if !validIssueStates[query.State] {
		return RequestPlan{}, p.operationError("list", "invalid_argument", fmt.Errorf("unsupported issue state filter %q", query.State))
	}

	if strings.TrimSpace(query.PageToken) != "" {
		u, err := p.parsePageToken(query.PageToken)
		if err != nil {
			return RequestPlan{}, p.operationError("list", "invalid_argument", err)
		}
		return buildPlan("list", http.MethodGet, u, nil)
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues")
	values := u.Query()
	state := query.State
	if state == "" {
		state = issuecore.IssueStateFilterOpen
	}
	values.Set("state", string(state))
	if labels := normalizeSet(query.Labels); len(labels) > 0 {
		values.Set("labels", strings.Join(labels, ","))
	}
	if assignee := strings.TrimSpace(query.Assignee); assignee != "" {
		values.Set("assignee", assignee)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	values.Set("per_page", strconv.Itoa(limit))
	u.RawQuery = values.Encode()
	return buildPlan("list", http.MethodGet, u, nil)
}

func (p *Provider) PlanGetIssue(locator issuecore.IssueLocator) (RequestPlan, error) {
	repo, number, err := p.resolveIssue(locator, "get")
	if err != nil {
		return RequestPlan{}, err
	}
	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number))
	return buildPlan("get", http.MethodGet, u, nil)
}

func (p *Provider) PlanCreateIssue(input issuecore.CreateIssueInput) (RequestPlan, error) {
	repo, err := parseRepository(input.Repository)
	if err != nil {
		return RequestPlan{}, p.operationError("create", "invalid_argument", err)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return RequestPlan{}, p.operationError("create", "invalid_argument", fmt.Errorf("issue title is required"))
	}

	payload := map[string]any{"title": title}
	if body := input.Body; strings.TrimSpace(body) != "" {
		payload["body"] = body
	}
	if labels := normalizeSet(input.Labels); len(labels) > 0 {
		payload["labels"] = labels
	}
	if assignees := normalizeSet(input.Assignees); len(assignees) > 0 {
		payload["assignees"] = assignees
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues")
	return buildPlan("create", http.MethodPost, u, payload)
}

func (p *Provider) PlanUpdateIssue(locator issuecore.IssueLocator, patch issuecore.IssuePatch) (RequestPlan, error) {
	repo, number, err := p.resolveIssue(locator, "update")
	if err != nil {
		return RequestPlan{}, err
	}
	if emptyPatch(patch) {
		return RequestPlan{}, p.operationError("update", "invalid_argument", fmt.Errorf("issue patch requires at least one field"))
	}
	if patch.StateReason != nil {
		return RequestPlan{}, p.operationError("update", "invalid_argument", fmt.Errorf("github provider only accepts state_reason via close or reopen"))
	}

	payload := map[string]any{}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" {
			return RequestPlan{}, p.operationError("update", "invalid_argument", fmt.Errorf("issue title cannot be empty"))
		}
		payload["title"] = title
	}
	if patch.Body != nil {
		payload["body"] = *patch.Body
	}
	if patch.Labels != nil {
		payload["labels"] = normalizeSet(*patch.Labels)
	}
	if patch.Assignees != nil {
		payload["assignees"] = normalizeSet(*patch.Assignees)
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number))
	return buildPlan("update", http.MethodPatch, u, payload)
}

func (p *Provider) PlanAddComment(locator issuecore.IssueLocator, input issuecore.AddCommentInput) (RequestPlan, error) {
	repo, number, err := p.resolveIssue(locator, "comment")
	if err != nil {
		return RequestPlan{}, err
	}
	if strings.TrimSpace(input.Body) == "" {
		return RequestPlan{}, p.operationError("comment", "invalid_argument", fmt.Errorf("comment body is required"))
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number) + "/comments")
	return buildPlan("comment", http.MethodPost, u, map[string]any{"body": input.Body})
}

func (p *Provider) PlanCloseIssue(locator issuecore.IssueLocator, input issuecore.CloseIssueInput) (RequestPlan, error) {
	repo, number, err := p.resolveIssue(locator, "close")
	if err != nil {
		return RequestPlan{}, err
	}
	reason, err := issuecore.NormalizeCloseStateReason(input.Reason)
	if err != nil {
		return RequestPlan{}, p.operationError("close", "invalid_argument", err)
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number))
	return buildPlan("close", http.MethodPatch, u, map[string]any{
		"state":        issuecore.IssueStateClosed,
		"state_reason": reason,
	})
}

func (p *Provider) PlanReopenIssue(locator issuecore.IssueLocator, input issuecore.ReopenIssueInput) (RequestPlan, error) {
	repo, number, err := p.resolveIssue(locator, "reopen")
	if err != nil {
		return RequestPlan{}, err
	}
	reason, err := issuecore.NormalizeReopenStateReason(input.Reason)
	if err != nil {
		return RequestPlan{}, p.operationError("reopen", "invalid_argument", err)
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number))
	return buildPlan("reopen", http.MethodPatch, u, map[string]any{
		"state":        issuecore.IssueStateOpen,
		"state_reason": reason,
	})
}

func (p *Provider) ListIssues(ctx context.Context, query issuecore.ListIssuesQuery) (issuecore.IssuePage, error) {
	plan, err := p.PlanListIssues(query)
	if err != nil {
		return issuecore.IssuePage{}, err
	}

	resp, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.IssuePage{}, err
	}

	issues, err := decodeIssueList(body, query.Repository)
	if err != nil {
		return issuecore.IssuePage{}, p.operationError("list", "upstream_error", err)
	}

	return issuecore.IssuePage{
		Issues:        issues,
		NextPageToken: nextPageToken(resp.Header.Get("Link")),
	}, nil
}

func (p *Provider) GetIssue(ctx context.Context, locator issuecore.IssueLocator) (issuecore.Issue, error) {
	plan, err := p.PlanGetIssue(locator)
	if err != nil {
		return issuecore.Issue{}, err
	}

	_, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.Issue{}, err
	}

	issue, repo, number, err := decodeIssue(body, locator.Repository)
	if err != nil {
		return issuecore.Issue{}, p.operationError("get", "upstream_error", err)
	}

	if issue.Comments > 0 {
		comments, err := p.loadIssueComments(ctx, repo, number)
		if err != nil {
			return issuecore.Issue{}, err
		}
		issue.CommentItems = comments
	}

	timeline, linkedPRs, err := p.loadIssueTimeline(ctx, repo, number)
	if err != nil {
		return issuecore.Issue{}, err
	}
	issue.Timeline = timeline
	issue.LinkedPullRequests = linkedPRs
	return issue, nil
}

func (p *Provider) CreateIssue(ctx context.Context, input issuecore.CreateIssueInput) (issuecore.Issue, error) {
	plan, err := p.PlanCreateIssue(input)
	if err != nil {
		return issuecore.Issue{}, err
	}

	_, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.Issue{}, err
	}
	issue, _, _, err := decodeIssue(body, input.Repository)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "upstream_error", err)
	}
	return issue, nil
}

func (p *Provider) UpdateIssue(ctx context.Context, locator issuecore.IssueLocator, patch issuecore.IssuePatch) (issuecore.Issue, error) {
	plan, err := p.PlanUpdateIssue(locator, patch)
	if err != nil {
		return issuecore.Issue{}, err
	}

	_, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.Issue{}, err
	}
	issue, _, _, err := decodeIssue(body, locator.Repository)
	if err != nil {
		return issuecore.Issue{}, p.operationError("update", "upstream_error", err)
	}
	return issue, nil
}

func (p *Provider) AddComment(ctx context.Context, locator issuecore.IssueLocator, input issuecore.AddCommentInput) (issuecore.Comment, error) {
	plan, err := p.PlanAddComment(locator, input)
	if err != nil {
		return issuecore.Comment{}, err
	}

	_, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.Comment{}, err
	}
	comment, err := decodeComment(body)
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "upstream_error", err)
	}
	return comment, nil
}

func (p *Provider) CloseIssue(ctx context.Context, locator issuecore.IssueLocator, input issuecore.CloseIssueInput) (issuecore.Issue, error) {
	plan, err := p.PlanCloseIssue(locator, input)
	if err != nil {
		return issuecore.Issue{}, err
	}

	_, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.Issue{}, err
	}
	issue, _, _, err := decodeIssue(body, locator.Repository)
	if err != nil {
		return issuecore.Issue{}, p.operationError("close", "upstream_error", err)
	}
	return issue, nil
}

func (p *Provider) ReopenIssue(ctx context.Context, locator issuecore.IssueLocator, input issuecore.ReopenIssueInput) (issuecore.Issue, error) {
	plan, err := p.PlanReopenIssue(locator, input)
	if err != nil {
		return issuecore.Issue{}, err
	}

	_, body, err := p.do(ctx, plan)
	if err != nil {
		return issuecore.Issue{}, err
	}
	issue, _, _, err := decodeIssue(body, locator.Repository)
	if err != nil {
		return issuecore.Issue{}, p.operationError("reopen", "upstream_error", err)
	}
	return issue, nil
}

func (p *Provider) planTimeline(repo repositoryRef, number int, pageToken string) (RequestPlan, error) {
	if strings.TrimSpace(pageToken) != "" {
		u, err := p.parsePageToken(pageToken)
		if err != nil {
			return RequestPlan{}, p.operationError("get", "invalid_argument", err)
		}
		return buildPlan("get", http.MethodGet, u, nil)
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number) + "/timeline")
	values := u.Query()
	values.Set("per_page", "100")
	u.RawQuery = values.Encode()
	return buildPlan("get", http.MethodGet, u, nil)
}

func (p *Provider) planComments(repo repositoryRef, number int, pageToken string) (RequestPlan, error) {
	if strings.TrimSpace(pageToken) != "" {
		u, err := p.parsePageToken(pageToken)
		if err != nil {
			return RequestPlan{}, p.operationError("get", "invalid_argument", err)
		}
		return buildPlan("get", http.MethodGet, u, nil)
	}

	u := p.endpointURL("repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Repo) + "/issues/" + strconv.Itoa(number) + "/comments")
	values := u.Query()
	values.Set("per_page", "100")
	u.RawQuery = values.Encode()
	return buildPlan("get", http.MethodGet, u, nil)
}

func (p *Provider) loadIssueComments(ctx context.Context, repo repositoryRef, number int) ([]issuecore.Comment, error) {
	comments := make([]issuecore.Comment, 0)
	pageToken := ""

	for {
		plan, err := p.planComments(repo, number, pageToken)
		if err != nil {
			return nil, err
		}

		resp, body, err := p.do(ctx, plan)
		if err != nil {
			return nil, err
		}

		pageComments, err := decodeComments(body)
		if err != nil {
			return nil, p.operationError("get", "upstream_error", err)
		}
		comments = append(comments, pageComments...)

		pageToken = nextPageToken(resp.Header.Get("Link"))
		if pageToken == "" {
			break
		}
	}

	return comments, nil
}

func (p *Provider) loadIssueTimeline(ctx context.Context, repo repositoryRef, number int) ([]issuecore.TimelineEvent, []issuecore.PullRequestRef, error) {
	timeline := make([]issuecore.TimelineEvent, 0)
	linkedPRs := make([]issuecore.PullRequestRef, 0)
	pageToken := ""

	for {
		plan, err := p.planTimeline(repo, number, pageToken)
		if err != nil {
			return nil, nil, err
		}

		resp, body, err := p.do(ctx, plan)
		if err != nil {
			return nil, nil, err
		}

		pageTimeline, pageLinkedPRs, err := decodeTimeline(body)
		if err != nil {
			return nil, nil, p.operationError("get", "upstream_error", err)
		}
		timeline = append(timeline, pageTimeline...)
		linkedPRs = append(linkedPRs, pageLinkedPRs...)

		pageToken = nextPageToken(resp.Header.Get("Link"))
		if pageToken == "" {
			break
		}
	}

	return timeline, dedupePullRequests(linkedPRs), nil
}

func (p *Provider) do(ctx context.Context, plan RequestPlan) (*http.Response, []byte, error) {
	if err := p.requireToken(plan.Operation); err != nil {
		return nil, nil, err
	}

	var bodyReader io.Reader
	if len(plan.Body) > 0 {
		bodyReader = bytes.NewReader(plan.Body)
	}

	req, err := http.NewRequestWithContext(ctx, plan.Method, plan.URL, bodyReader)
	if err != nil {
		return nil, nil, p.operationError(plan.Operation, "upstream_error", err)
	}
	for key, value := range plan.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, nil, p.operationError(plan.Operation, "upstream_error", err)
	}

	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return resp, nil, p.operationError(plan.Operation, "upstream_error", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, body, p.apiError(plan.Operation, resp, body)
	}

	return resp, body, nil
}

func (p *Provider) apiError(operation string, resp *http.Response, body []byte) error {
	payload := apiErrorPayload{}
	_ = json.Unmarshal(body, &payload)

	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = resp.Status
	}
	if errorsText := strings.TrimSpace(string(payload.Errors)); errorsText != "" && errorsText != "null" {
		message = message + " (" + errorsText + ")"
	}
	if doc := strings.TrimSpace(payload.DocumentationURL); doc != "" {
		message = message + " [" + doc + "]"
	}
	if sso := strings.TrimSpace(resp.Header.Get("X-GitHub-SSO")); sso != "" {
		message = message + " [x-github-sso: " + sso + "]"
	}

	code := "upstream_error"
	switch {
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		code = "invalid_argument"
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		code = "not_found"
	case isRateLimited(resp, payload.Message):
		code = "rate_limited"
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		code = "authentication_error"
	}

	return p.operationError(operation, code, fmt.Errorf("status %d: %s", resp.StatusCode, message))
}

func (p *Provider) requireToken(operation string) error {
	if strings.TrimSpace(p.token) == "" {
		return p.operationError(operation, "provider_config_error", fmt.Errorf("github provider token is required (use --github-token, %s, %s, or %s)", EnvToken, EnvTokenGH, EnvTokenGitHub))
	}
	return nil
}

func (p *Provider) endpointURL(relative string) *url.URL {
	u := *p.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/" + strings.TrimLeft(relative, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return &u
}

func (p *Provider) parsePageToken(token string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(token))
	if err != nil {
		return nil, fmt.Errorf("invalid page token %q", token)
	}
	if !u.IsAbs() {
		return p.baseURL.ResolveReference(u), nil
	}
	if !strings.EqualFold(u.Scheme, p.baseURL.Scheme) || !strings.EqualFold(u.Host, p.baseURL.Host) {
		return nil, fmt.Errorf("page token host %q does not match configured GitHub API host %q", u.Host, p.baseURL.Host)
	}
	return u, nil
}

func (p *Provider) resolveIssue(locator issuecore.IssueLocator, operation string) (repositoryRef, int, error) {
	repository := strings.TrimSpace(locator.Repository)

	var (
		repo    repositoryRef
		hasRepo bool
		err     error
	)
	if repository != "" {
		repo, err = parseRepository(repository)
		if err != nil {
			return repositoryRef{}, 0, p.operationError(operation, "invalid_argument", err)
		}
		hasRepo = true
	}

	if locator.Number > 0 {
		if !hasRepo {
			return repositoryRef{}, 0, p.operationError(operation, "invalid_argument", fmt.Errorf("github provider requires a repository when using a numeric issue identifier"))
		}
		return repo, locator.Number, nil
	}

	id := strings.TrimSpace(locator.ID)
	if id == "" {
		return repositoryRef{}, 0, p.operationError(operation, "invalid_argument", fmt.Errorf("issue locator requires a numeric issue identifier or issue URL"))
	}
	if number, err := strconv.Atoi(id); err == nil && number > 0 {
		if !hasRepo {
			return repositoryRef{}, 0, p.operationError(operation, "invalid_argument", fmt.Errorf("github provider requires a repository when using a numeric issue identifier"))
		}
		return repo, number, nil
	}

	if urlRepo, number, ok := p.parseIssueURL(id); ok {
		if hasRepo && !sameRepository(repo, urlRepo) {
			return repositoryRef{}, 0, p.operationError(operation, "invalid_argument", fmt.Errorf("issue URL repository %q does not match locator repository %q", urlRepo.String(), repo.String()))
		}
		return urlRepo, number, nil
	}

	return repositoryRef{}, 0, p.operationError(operation, "invalid_argument", fmt.Errorf("github provider requires a numeric issue identifier or issue URL"))
}

func (p *Provider) operationError(operation, code string, err error) error {
	return &issuecore.OperationError{
		Code:      code,
		Provider:  issuecore.ProviderGitHub,
		Operation: operation,
		Err:       err,
	}
}

func buildPlan(operation, method string, u *url.URL, payload any) (RequestPlan, error) {
	plan := RequestPlan{
		Operation: operation,
		Method:    method,
		URL:       u.String(),
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": defaultAPIVersion,
			"User-Agent":           defaultUserAgent,
		},
	}
	if payload == nil {
		return plan, nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return RequestPlan{}, err
	}
	plan.Body = json.RawMessage(raw)
	plan.Headers["Content-Type"] = "application/json"
	return plan, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = defaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub API base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("GitHub API base URL must include scheme and host")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u, nil
}

func parseRepository(raw string) (repositoryRef, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return repositoryRef{}, fmt.Errorf("github operations require --repository owner/repo")
	}

	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		u, err := url.Parse(value)
		if err != nil {
			return repositoryRef{}, fmt.Errorf("invalid repository %q", raw)
		}
		value = strings.Trim(u.Path, "/")
	}

	parts := strings.Split(strings.Trim(value, "/"), "/")
	switch len(parts) {
	case 2:
		return repositoryRef{Owner: parts[0], Repo: parts[1]}, nil
	case 3:
		return repositoryRef{Owner: parts[1], Repo: parts[2]}, nil
	default:
		return repositoryRef{}, fmt.Errorf("invalid repository %q; want owner/repo", raw)
	}
}

func decodeIssueList(body []byte, fallbackRepository string) ([]issuecore.Issue, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(body, &rawItems); err != nil {
		return nil, fmt.Errorf("decode issue list: %w", err)
	}

	issues := make([]issuecore.Issue, 0, len(rawItems))
	for _, rawItem := range rawItems {
		issue, _, _, err := decodeIssue(rawItem, fallbackRepository)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func decodeIssue(body []byte, fallbackRepository string) (issuecore.Issue, repositoryRef, int, error) {
	wire := wireIssue{}
	if err := json.Unmarshal(body, &wire); err != nil {
		return issuecore.Issue{}, repositoryRef{}, 0, fmt.Errorf("decode issue: %w", err)
	}

	issue := issuecore.Issue{
		ID:                scalarID(wire.ID),
		NodeID:            wire.NodeID,
		Number:            wire.Number,
		URL:               wire.URL,
		HTMLURL:           wire.HTMLURL,
		Title:             wire.Title,
		Body:              wire.Body,
		BodyText:          wire.BodyText,
		State:             wire.State,
		StateReason:       wire.StateReason,
		User:              wire.User.toActor(),
		AuthorAssociation: wire.AuthorAssociation,
		Labels:            toLabels(wire.Labels),
		Milestone:         wire.Milestone.toMilestone(),
		Assignee:          wire.Assignee.toActor(),
		Assignees:         toActors(wire.Assignees),
		Comments:          wire.Comments,
		Locked:            wire.Locked,
		ActiveLockReason:  wire.ActiveLockReason,
		Reactions:         wire.Reactions.toReactionRollup(),
		CreatedAt:         wire.CreatedAt,
		UpdatedAt:         wire.UpdatedAt,
		ClosedAt:          wire.ClosedAt,
		ClosedBy:          wire.ClosedBy.toActor(),
		ProviderRaw:       cloneRaw(body),
	}
	issue.Repository = repositoryFromAPIURL(wire.RepositoryURL)
	if issue.Repository == "" {
		issue.Repository = strings.TrimSpace(fallbackRepository)
	}
	issue.PullRequest = wire.PullRequest.toPullRequestRef(issue)

	repo, err := parseRepository(issue.Repository)
	if err != nil {
		return issuecore.Issue{}, repositoryRef{}, 0, err
	}
	return issue, repo, issue.Number, nil
}

func decodeComment(body []byte) (issuecore.Comment, error) {
	wire := wireComment{}
	if err := json.Unmarshal(body, &wire); err != nil {
		return issuecore.Comment{}, fmt.Errorf("decode comment: %w", err)
	}
	comment := issuecore.Comment{
		ID:                scalarID(wire.ID),
		NodeID:            wire.NodeID,
		URL:               wire.URL,
		HTMLURL:           wire.HTMLURL,
		Body:              wire.Body,
		BodyText:          wire.BodyText,
		User:              wire.User.toActor(),
		AuthorAssociation: wire.AuthorAssociation,
		CreatedAt:         wire.CreatedAt,
		UpdatedAt:         wire.UpdatedAt,
		Reactions:         wire.Reactions.toReactionRollup(),
		Pinned:            wire.Pinned,
		PinnedAt:          wire.PinnedAt,
		PinnedBy:          wire.PinnedBy.toActor(),
		MinimizedReason:   wire.MinimizedReason,
		ProviderRaw:       cloneRaw(body),
	}
	return comment, nil
}

func decodeComments(body []byte) ([]issuecore.Comment, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(body, &rawItems); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}

	comments := make([]issuecore.Comment, 0, len(rawItems))
	for _, rawItem := range rawItems {
		comment, err := decodeComment(rawItem)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, nil
}

func decodeTimeline(body []byte) ([]issuecore.TimelineEvent, []issuecore.PullRequestRef, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(body, &rawItems); err != nil {
		return nil, nil, fmt.Errorf("decode timeline: %w", err)
	}

	events := make([]issuecore.TimelineEvent, 0, len(rawItems))
	linkedPRs := make([]issuecore.PullRequestRef, 0)
	for _, rawItem := range rawItems {
		wire := timelineEventResponse{}
		if err := json.Unmarshal(rawItem, &wire); err != nil {
			return nil, nil, fmt.Errorf("decode timeline event: %w", err)
		}

		payload := map[string]any{}
		if wire.CommitID != "" {
			payload["commit_id"] = wire.CommitID
			payload["commit_url"] = wire.CommitURL
		}
		if wire.Source != nil && wire.Source.Issue != nil {
			sourceIssue, _, _, err := decodeIssue(mustMarshal(wire.Source.Issue), "")
			if err == nil {
				payload["source"] = sourceIssue
				if sourceIssue.PullRequest != nil {
					linkedPRs = append(linkedPRs, *sourceIssue.PullRequest)
				}
			}
		}
		if wire.Rename != nil {
			payload["rename"] = wire.Rename
		}
		if wire.UpdatedAt != nil {
			payload["updated_at"] = wire.UpdatedAt
		}

		events = append(events, issuecore.TimelineEvent{
			ID:          scalarID(wire.ID),
			NodeID:      wire.NodeID,
			Kind:        wire.Event,
			Actor:       wire.Actor.toActor(),
			CreatedAt:   wire.CreatedAt,
			Payload:     marshalPayload(payload),
			ProviderRaw: cloneRaw(rawItem),
		})
	}

	return events, dedupePullRequests(linkedPRs), nil
}

func nextPageToken(linkHeader string) string {
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return strings.TrimSpace(part[start+1 : end])
		}
	}
	return ""
}

func (p *Provider) parseIssueURL(raw string) (repositoryRef, int, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() {
		return repositoryRef{}, 0, false
	}

	host := normalizeURLHost(u.Host)
	if host == "" {
		return repositoryRef{}, 0, false
	}

	baseHost := ""
	var basePath []string
	if p != nil && p.baseURL != nil {
		baseHost = normalizeURLHost(p.baseURL.Host)
		basePath = pathSegments(p.baseURL.Path)
	}

	parts := pathSegments(u.Path)
	if host == "github.com" || (baseHost != "" && host == baseHost && baseHost != "api.github.com") {
		if repo, number, ok := parseHTMLIssuePath(parts); ok {
			return repo, number, true
		}
	}

	if baseHost != "" && host == baseHost {
		if repo, number, ok := parseAPIIssuePath(stripPathPrefix(parts, basePath)); ok {
			return repo, number, true
		}
	}

	return repositoryRef{}, 0, false
}

func normalizeURLHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return host
}

func pathSegments(raw string) []string {
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "/")
}

func stripPathPrefix(parts, prefix []string) []string {
	if len(prefix) == 0 {
		return parts
	}
	if len(parts) < len(prefix) {
		return nil
	}
	for i := range prefix {
		if parts[i] != prefix[i] {
			return nil
		}
	}
	return parts[len(prefix):]
}

func parseAPIIssuePath(parts []string) (repositoryRef, int, bool) {
	if len(parts) != 5 || parts[0] != "repos" || parts[3] != "issues" {
		return repositoryRef{}, 0, false
	}
	return repositoryAndNumberFromParts(parts[1], parts[2], parts[4])
}

func parseHTMLIssuePath(parts []string) (repositoryRef, int, bool) {
	if len(parts) != 4 || (parts[2] != "issues" && parts[2] != "pull") {
		return repositoryRef{}, 0, false
	}
	return repositoryAndNumberFromParts(parts[0], parts[1], parts[3])
}

func repositoryAndNumberFromParts(owner, repo, rawNumber string) (repositoryRef, int, bool) {
	number, err := strconv.Atoi(rawNumber)
	if err != nil || number <= 0 {
		return repositoryRef{}, 0, false
	}
	return repositoryRef{Owner: owner, Repo: repo}, number, true
}

func sameRepository(left, right repositoryRef) bool {
	return strings.EqualFold(left.Owner, right.Owner) && strings.EqualFold(left.Repo, right.Repo)
}

func repositoryFromAPIURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "repos" {
		return parts[1] + "/" + parts[2]
	}
	return ""
}

func emptyPatch(patch issuecore.IssuePatch) bool {
	return patch.Title == nil &&
		patch.Body == nil &&
		patch.Labels == nil &&
		patch.Assignees == nil &&
		patch.StateReason == nil
}

func normalizeSet(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func scalarID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return strconv.FormatInt(number, 10)
	}
	return string(raw)
}

func marshalPayload(payload map[string]any) json.RawMessage {
	if len(payload) == 0 {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return json.RawMessage(raw)
}

func mustMarshal(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

func cloneRaw(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(append([]byte(nil), raw...))
}

func dedupePullRequests(values []issuecore.PullRequestRef) []issuecore.PullRequestRef {
	if len(values) == 0 {
		return nil
	}
	type key struct {
		Repository string
		Number     int
		URL        string
	}
	seen := map[key]issuecore.PullRequestRef{}
	for _, value := range values {
		current := value
		if current.Repository == "" && current.URL != "" {
			current.Repository = repositoryFromAPIURL(current.URL)
		}
		identifier := key{
			Repository: current.Repository,
			Number:     current.Number,
			URL:        current.URL,
		}
		if identifier.Repository == "" && identifier.Number == 0 && identifier.URL == "" {
			continue
		}
		seen[identifier] = current
	}

	out := make([]issuecore.PullRequestRef, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repository != out[j].Repository {
			return out[i].Repository < out[j].Repository
		}
		if out[i].Number != out[j].Number {
			return out[i].Number < out[j].Number
		}
		return out[i].URL < out[j].URL
	})
	return out
}

func isRateLimited(resp *http.Response, message string) bool {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return false
	}
	if strings.TrimSpace(resp.Header.Get("Retry-After")) != "" {
		return true
	}
	if strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
		return true
	}
	return strings.Contains(strings.ToLower(message), "rate limit")
}

func (w *wireActor) toActor() *issuecore.Actor {
	if w == nil {
		return nil
	}
	actor := issuecore.Actor{
		ID:      scalarID(w.ID),
		NodeID:  w.NodeID,
		Login:   w.Login,
		Type:    w.Type,
		URL:     w.URL,
		HTMLURL: w.HTMLURL,
	}
	if actor.ID == "" && actor.NodeID == "" && actor.Login == "" && actor.Type == "" && actor.URL == "" && actor.HTMLURL == "" {
		return nil
	}
	return &actor
}

func toActors(values []wireActor) []issuecore.Actor {
	if len(values) == 0 {
		return nil
	}
	actors := make([]issuecore.Actor, 0, len(values))
	for _, value := range values {
		if actor := (&value).toActor(); actor != nil {
			actors = append(actors, *actor)
		}
	}
	if len(actors) == 0 {
		return nil
	}
	return actors
}

func toLabels(values []wireLabel) []issuecore.Label {
	if len(values) == 0 {
		return nil
	}
	labels := make([]issuecore.Label, 0, len(values))
	for _, value := range values {
		labels = append(labels, issuecore.Label{
			ID:          scalarID(value.ID),
			NodeID:      value.NodeID,
			Name:        value.Name,
			Color:       value.Color,
			Description: value.Description,
			IsDefault:   value.IsDefault,
		})
	}
	return labels
}

func (w *wireMilestone) toMilestone() *issuecore.Milestone {
	if w == nil {
		return nil
	}
	return &issuecore.Milestone{
		ID:          scalarID(w.ID),
		NodeID:      w.NodeID,
		Number:      w.Number,
		Title:       w.Title,
		Description: w.Description,
		State:       w.State,
		DueOn:       w.DueOn,
	}
}

func (w *wireReactionRollup) toReactionRollup() *issuecore.ReactionRollup {
	if w == nil {
		return nil
	}
	return &issuecore.ReactionRollup{
		URL:        w.URL,
		TotalCount: w.TotalCount,
		PlusOne:    w.PlusOne,
		MinusOne:   w.MinusOne,
		Laugh:      w.Laugh,
		Confused:   w.Confused,
		Heart:      w.Heart,
		Hooray:     w.Hooray,
		Rocket:     w.Rocket,
		Eyes:       w.Eyes,
	}
}

func (w *wirePullRequest) toPullRequestRef(issue issuecore.Issue) *issuecore.PullRequestRef {
	if w == nil {
		return nil
	}

	pr := &issuecore.PullRequestRef{
		Number:     issue.Number,
		ID:         issue.ID,
		NodeID:     issue.NodeID,
		Repository: issue.Repository,
		URL:        firstNonEmpty(w.URL, issue.URL),
		HTMLURL:    firstNonEmpty(w.HTMLURL, issue.HTMLURL),
		DiffURL:    w.DiffURL,
		PatchURL:   w.PatchURL,
		MergedAt:   w.MergedAt,
	}
	switch {
	case w.MergedAt != nil:
		pr.State = issuecore.PullRequestStateMerged
	case issue.State == issuecore.IssueStateClosed:
		pr.State = issuecore.PullRequestStateClosed
	default:
		pr.State = issuecore.PullRequestStateOpen
	}
	return pr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
