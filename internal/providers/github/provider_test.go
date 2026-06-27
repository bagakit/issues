package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/bagakit/issues/pkg/issuecore"
)

type doerFunc func(req *http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type scriptedDoer struct {
	t        *testing.T
	handlers []doerFunc
	calls    int
}

func (d *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	if d.calls >= len(d.handlers) {
		d.t.Fatalf("unexpected request %s %s", req.Method, req.URL.String())
	}
	handler := d.handlers[d.calls]
	d.calls++
	return handler(req)
}

func TestPlanListIssuesBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, "test-token", nil)

	plan, err := provider.PlanListIssues(issuecore.ListIssuesQuery{
		Repository: "bagakit/issues",
		State:      issuecore.IssueStateFilterClosed,
		Labels:     []string{"zeta", "alpha", "alpha"},
		Assignee:   "alice",
		Limit:      150,
	})
	if err != nil {
		t.Fatalf("plan list issues: %v", err)
	}

	if plan.Operation != "list" {
		t.Fatalf("unexpected operation: %q", plan.Operation)
	}
	if plan.Method != http.MethodGet {
		t.Fatalf("unexpected method: %q", plan.Method)
	}
	if len(plan.Body) != 0 {
		t.Fatalf("list plan should not include a body: %s", plan.Body)
	}
	if plan.Headers["Accept"] != "application/vnd.github+json" {
		t.Fatalf("unexpected accept header: %#v", plan.Headers)
	}
	if plan.Headers["X-GitHub-Api-Version"] != defaultAPIVersion {
		t.Fatalf("unexpected api version header: %#v", plan.Headers)
	}

	parsedURL, err := url.Parse(plan.URL)
	if err != nil {
		t.Fatalf("parse planned url: %v", err)
	}
	if parsedURL.Path != "/repos/bagakit/issues/issues" {
		t.Fatalf("unexpected path: %q", parsedURL.Path)
	}
	if got := parsedURL.Query().Get("state"); got != "closed" {
		t.Fatalf("unexpected state query: %q", got)
	}
	if got := parsedURL.Query().Get("labels"); got != "alpha,zeta" {
		t.Fatalf("unexpected labels query: %q", got)
	}
	if got := parsedURL.Query().Get("assignee"); got != "alice" {
		t.Fatalf("unexpected assignee query: %q", got)
	}
	if got := parsedURL.Query().Get("per_page"); got != "100" {
		t.Fatalf("unexpected per_page query: %q", got)
	}
}

func TestPlanListIssuesUsesOpaquePageToken(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, "test-token", nil)
	pageToken := "https://api.github.com/repos/bagakit/issues/issues?per_page=30&page=2"

	plan, err := provider.PlanListIssues(issuecore.ListIssuesQuery{
		Repository: "bagakit/issues",
		PageToken:  pageToken,
	})
	if err != nil {
		t.Fatalf("plan paged list issues: %v", err)
	}
	if plan.URL != pageToken {
		t.Fatalf("unexpected paged plan url: %q", plan.URL)
	}
}

func TestPlanMutationsBuildExpectedRequests(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, "test-token", nil)

	tests := []struct {
		name    string
		plan    func() (RequestPlan, error)
		method  string
		path    string
		body    map[string]any
		hasBody bool
	}{
		{
			name: "get",
			plan: func() (RequestPlan, error) {
				return provider.PlanGetIssue(issuecore.IssueLocator{Repository: "bagakit/issues", Number: 7})
			},
			method:  http.MethodGet,
			path:    "/repos/bagakit/issues/issues/7",
			hasBody: false,
		},
		{
			name: "create",
			plan: func() (RequestPlan, error) {
				return provider.PlanCreateIssue(issuecore.CreateIssueInput{
					Repository: "bagakit/issues",
					Title:      "ship it",
					Body:       "body",
					Labels:     []string{"zeta", "alpha"},
					Assignees:  []string{"bob", "alice"},
				})
			},
			method: http.MethodPost,
			path:   "/repos/bagakit/issues/issues",
			body: map[string]any{
				"title":     "ship it",
				"body":      "body",
				"labels":    []any{"alpha", "zeta"},
				"assignees": []any{"alice", "bob"},
			},
			hasBody: true,
		},
		{
			name: "update",
			plan: func() (RequestPlan, error) {
				return provider.PlanUpdateIssue(issuecore.IssueLocator{Repository: "bagakit/issues", Number: 9}, issuecore.IssuePatch{
					Title:     stringPtr("updated title"),
					Assignees: slicePtr([]string{"bob", "alice"}),
				})
			},
			method: http.MethodPatch,
			path:   "/repos/bagakit/issues/issues/9",
			body: map[string]any{
				"title":     "updated title",
				"assignees": []any{"alice", "bob"},
			},
			hasBody: true,
		},
		{
			name: "comment",
			plan: func() (RequestPlan, error) {
				return provider.PlanAddComment(issuecore.IssueLocator{Repository: "bagakit/issues", Number: 9}, issuecore.AddCommentInput{
					Body: "first comment",
				})
			},
			method: http.MethodPost,
			path:   "/repos/bagakit/issues/issues/9/comments",
			body: map[string]any{
				"body": "first comment",
			},
			hasBody: true,
		},
		{
			name: "close",
			plan: func() (RequestPlan, error) {
				return provider.PlanCloseIssue(issuecore.IssueLocator{Repository: "bagakit/issues", Number: 9}, issuecore.CloseIssueInput{
					Reason: issuecore.IssueStateReasonNotPlanned,
				})
			},
			method: http.MethodPatch,
			path:   "/repos/bagakit/issues/issues/9",
			body: map[string]any{
				"state":        "closed",
				"state_reason": "not_planned",
			},
			hasBody: true,
		},
		{
			name: "reopen",
			plan: func() (RequestPlan, error) {
				return provider.PlanReopenIssue(issuecore.IssueLocator{Repository: "bagakit/issues", Number: 9}, issuecore.ReopenIssueInput{})
			},
			method: http.MethodPatch,
			path:   "/repos/bagakit/issues/issues/9",
			body: map[string]any{
				"state":        "open",
				"state_reason": "reopened",
			},
			hasBody: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan, err := tt.plan()
			if err != nil {
				t.Fatalf("build plan: %v", err)
			}
			if plan.Method != tt.method {
				t.Fatalf("unexpected method: got %q want %q", plan.Method, tt.method)
			}
			parsedURL, err := url.Parse(plan.URL)
			if err != nil {
				t.Fatalf("parse planned url: %v", err)
			}
			if parsedURL.Path != tt.path {
				t.Fatalf("unexpected path: got %q want %q", parsedURL.Path, tt.path)
			}
			if tt.hasBody {
				requireJSONBody(t, plan.Body, tt.body)
				if got := plan.Headers["Content-Type"]; got != "application/json" {
					t.Fatalf("unexpected content type header: %q", got)
				}
				return
			}
			if len(plan.Body) != 0 {
				t.Fatalf("unexpected request body: %s", plan.Body)
			}
		})
	}
}

func TestListIssuesUsesInjectedClientAndDecodesNumericIDs(t *testing.T) {
	t.Parallel()

	next := "https://api.github.com/repos/bagakit/issues/issues?per_page=1&page=2"
	provider := newTestProvider(t, "test-token", doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected method: %q", req.Method)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("unexpected accept header: %q", got)
		}
		if got := req.Header.Get("X-GitHub-Api-Version"); got != defaultAPIVersion {
			t.Fatalf("unexpected api version header: %q", got)
		}
		if got := req.Header.Get("User-Agent"); got != defaultUserAgent {
			t.Fatalf("unexpected user agent header: %q", got)
		}
		return jsonResponse(http.StatusOK, map[string]string{
			"Link": fmt.Sprintf(`<%s>; rel="next"`, next),
		}, `[
			{
				"id": 101,
				"node_id": "I_kw123",
				"repository_url": "https://api.github.com/repos/bagakit/issues",
				"number": 42,
				"url": "https://api.github.com/repos/bagakit/issues/issues/42",
				"html_url": "https://github.com/bagakit/issues/pull/42",
				"title": "Ship GitHub provider",
				"body": "Plan then execute",
				"state": "open",
				"user": {"id": 7, "login": "octocat", "type": "User"},
				"labels": [{"id": 9, "node_id": "LA_kw", "name": "bug", "color": "d73a4a"}],
				"comments": 2,
				"created_at": "2024-01-02T00:00:00Z",
				"updated_at": "2024-01-02T01:00:00Z",
				"pull_request": {
					"url": "https://api.github.com/repos/bagakit/issues/pulls/42",
					"html_url": "https://github.com/bagakit/issues/pull/42",
					"diff_url": "https://github.com/bagakit/issues/pull/42.diff",
					"patch_url": "https://github.com/bagakit/issues/pull/42.patch"
				}
			}
		]`)
	}))

	page, err := provider.ListIssues(context.Background(), issuecore.ListIssuesQuery{
		Repository: "bagakit/issues",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if page.NextPageToken != next {
		t.Fatalf("unexpected next page token: %q", page.NextPageToken)
	}
	if len(page.Issues) != 1 {
		t.Fatalf("unexpected issue count: %d", len(page.Issues))
	}
	issue := page.Issues[0]
	if issue.ID != "101" || issue.User == nil || issue.User.ID != "7" {
		t.Fatalf("numeric ids were not normalized: %+v", issue)
	}
	if issue.Repository != "bagakit/issues" {
		t.Fatalf("unexpected repository: %q", issue.Repository)
	}
	if len(issue.Labels) != 1 || issue.Labels[0].ID != "9" {
		t.Fatalf("unexpected labels: %+v", issue.Labels)
	}
	if issue.PullRequest == nil {
		t.Fatalf("expected pull request stub in issue payload")
	}
	if issue.PullRequest.Number != 42 || issue.PullRequest.Repository != "bagakit/issues" {
		t.Fatalf("unexpected pull request ref: %+v", issue.PullRequest)
	}
}

func TestGetIssueEnrichesTimelineWithLinkedPullRequests(t *testing.T) {
	t.Parallel()

	doer := &scriptedDoer{
		t: t,
		handlers: []doerFunc{
			func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/repos/bagakit/issues/issues/7" {
					t.Fatalf("unexpected issue path: %q", req.URL.Path)
				}
				return jsonResponse(http.StatusOK, nil, `{
					"id": 700,
					"node_id": "I_kw700",
					"repository_url": "https://api.github.com/repos/bagakit/issues",
					"number": 7,
					"url": "https://api.github.com/repos/bagakit/issues/issues/7",
					"html_url": "https://github.com/bagakit/issues/issues/7",
					"title": "Need provider review",
					"body": "Timeline should enrich links",
					"state": "open",
					"comments": 1,
					"user": {"id": 1, "login": "maintainer", "type": "User"},
					"created_at": "2024-01-02T00:00:00Z",
					"updated_at": "2024-01-02T01:00:00Z"
				}`)
			},
			func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/repos/bagakit/issues/issues/7/comments" {
					t.Fatalf("unexpected comments path: %q", req.URL.Path)
				}
				if got := req.URL.Query().Get("per_page"); got != "100" {
					t.Fatalf("unexpected comments per_page: %q", got)
				}
				return jsonResponse(http.StatusOK, nil, `[
					{
						"id": 901,
						"node_id": "IC_kw901",
						"url": "https://api.github.com/repos/bagakit/issues/issues/comments/901",
						"html_url": "https://github.com/bagakit/issues/issues/7#issuecomment-901",
						"body": "first follow-up",
						"user": {"id": 4, "login": "commenter", "type": "User"},
						"created_at": "2024-01-02T01:30:00Z",
						"updated_at": "2024-01-02T01:30:00Z"
					}
				]`)
			},
			func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/repos/bagakit/issues/issues/7/timeline" {
					t.Fatalf("unexpected timeline path: %q", req.URL.Path)
				}
				if got := req.URL.Query().Get("per_page"); got != "100" {
					t.Fatalf("unexpected timeline per_page: %q", got)
				}
				next := "https://api.github.com/repos/bagakit/issues/issues/7/timeline?per_page=100&page=2"
				return jsonResponse(http.StatusOK, map[string]string{
					"Link": fmt.Sprintf(`<%s>; rel="next"`, next),
				}, `[
					{
						"id": 1001,
						"node_id": "TE_kw1001",
						"event": "cross-referenced",
						"actor": {"id": 2, "login": "octocat", "type": "User"},
						"created_at": "2024-01-02T02:00:00Z",
						"source": {
							"type": "issue",
							"issue": {
								"id": 1200,
								"node_id": "PR_kw1200",
								"repository_url": "https://api.github.com/repos/bagakit/issues",
								"number": 12,
								"url": "https://api.github.com/repos/bagakit/issues/issues/12",
								"html_url": "https://github.com/bagakit/issues/pull/12",
								"title": "Land provider",
								"state": "open",
								"user": {"id": 3, "login": "reviewer", "type": "User"},
								"created_at": "2024-01-02T01:00:00Z",
								"updated_at": "2024-01-02T02:00:00Z",
								"pull_request": {
									"url": "https://api.github.com/repos/bagakit/issues/pulls/12",
									"html_url": "https://github.com/bagakit/issues/pull/12",
									"diff_url": "https://github.com/bagakit/issues/pull/12.diff",
									"patch_url": "https://github.com/bagakit/issues/pull/12.patch"
								}
							}
						}
					}
				]`)
			},
			func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://api.github.com/repos/bagakit/issues/issues/7/timeline?per_page=100&page=2" {
					t.Fatalf("unexpected paged timeline url: %q", req.URL.String())
				}
				return jsonResponse(http.StatusOK, nil, `[
					{
						"id": 1002,
						"node_id": "TE_kw1002",
						"event": "renamed",
						"actor": {"id": 2, "login": "octocat", "type": "User"},
						"created_at": "2024-01-02T03:00:00Z",
						"rename": {"from": "old title", "to": "new title"}
					}
				]`)
			},
		},
	}
	provider := newTestProvider(t, "test-token", doer)

	issue, err := provider.GetIssue(context.Background(), issuecore.IssueLocator{
		Repository: "bagakit/issues",
		Number:     7,
	})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if len(issue.Timeline) != 2 || issue.Timeline[0].Kind != "cross-referenced" || issue.Timeline[1].Kind != "renamed" {
		t.Fatalf("unexpected timeline: %+v", issue.Timeline)
	}
	if len(issue.CommentItems) != 1 || issue.CommentItems[0].ID != "901" {
		t.Fatalf("unexpected comments: %+v", issue.CommentItems)
	}
	if issue.Timeline[0].Actor == nil || issue.Timeline[0].Actor.ID != "2" {
		t.Fatalf("unexpected timeline actor: %+v", issue.Timeline[0].Actor)
	}
	if len(issue.LinkedPullRequests) != 1 {
		t.Fatalf("unexpected linked pull requests: %+v", issue.LinkedPullRequests)
	}
	linked := issue.LinkedPullRequests[0]
	if linked.Number != 12 || linked.Repository != "bagakit/issues" || linked.URL == "" {
		t.Fatalf("unexpected linked pull request: %+v", linked)
	}
}

func TestAddCommentUsesInjectedClientAndDecodesNumericIDs(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, "test-token", doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("unexpected method: %q", req.Method)
		}
		if req.URL.Path != "/repos/bagakit/issues/issues/7/comments" {
			t.Fatalf("unexpected comment path: %q", req.URL.Path)
		}
		requireJSONRequest(t, req, map[string]any{"body": "first comment"})
		return jsonResponse(http.StatusCreated, nil, `{
			"id": 501,
			"node_id": "IC_kw501",
			"url": "https://api.github.com/repos/bagakit/issues/issues/comments/501",
			"html_url": "https://github.com/bagakit/issues/issues/7#issuecomment-501",
			"body": "first comment",
			"user": {"id": 7, "login": "octocat", "type": "User"},
			"created_at": "2024-01-02T03:00:00Z",
			"updated_at": "2024-01-02T03:00:00Z"
		}`)
	}))

	comment, err := provider.AddComment(context.Background(), issuecore.IssueLocator{
		Repository: "bagakit/issues",
		Number:     7,
	}, issuecore.AddCommentInput{Body: "first comment"})
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if comment.ID != "501" || comment.User == nil || comment.User.ID != "7" {
		t.Fatalf("numeric ids were not normalized in comment: %+v", comment)
	}
}

func TestListIssuesMissingTokenSkipsHTTPClient(t *testing.T) {
	t.Parallel()

	called := false
	provider := newTestProvider(t, "", doerFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected network call")
	}))

	_, err := provider.ListIssues(context.Background(), issuecore.ListIssuesQuery{
		Repository: "bagakit/issues",
	})
	if err == nil {
		t.Fatalf("expected configuration error")
	}
	var opErr *issuecore.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected operation error, got %T", err)
	}
	if opErr.Code != "provider_config_error" {
		t.Fatalf("unexpected error code: %q", opErr.Code)
	}
	if called {
		t.Fatalf("expected provider to fail before touching the HTTP client")
	}
}

func TestListIssuesMapsGitHubErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		headers  map[string]string
		body     string
		wantCode string
	}{
		{
			name:     "authentication",
			status:   http.StatusUnauthorized,
			body:     `{"message":"Bad credentials"}`,
			wantCode: "authentication_error",
		},
		{
			name:   "rate-limited",
			status: http.StatusForbidden,
			headers: map[string]string{
				"Retry-After": "60",
			},
			body:     `{"message":"You have exceeded a secondary rate limit"}`,
			wantCode: "rate_limited",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider := newTestProvider(t, "test-token", doerFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(tt.status, tt.headers, tt.body)
			}))

			_, err := provider.ListIssues(context.Background(), issuecore.ListIssuesQuery{
				Repository: "bagakit/issues",
			})
			if err == nil {
				t.Fatalf("expected provider error")
			}
			var opErr *issuecore.OperationError
			if !errors.As(err, &opErr) {
				t.Fatalf("expected operation error, got %T", err)
			}
			if opErr.Code != tt.wantCode {
				t.Fatalf("unexpected error code: got %q want %q", opErr.Code, tt.wantCode)
			}
		})
	}
}

func newTestProvider(t *testing.T, token string, doer httpDoer) *Provider {
	t.Helper()

	provider, err := New(Config{Token: token})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if doer != nil {
		provider.client = doer
	}
	return provider
}

func jsonResponse(status int, headers map[string]string, body string) (*http.Response, error) {
	header := http.Header{}
	for key, value := range headers {
		header.Set(key, value)
	}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func requireJSONBody(t *testing.T, raw json.RawMessage, want map[string]any) {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode json body: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected json body:\n got: %#v\nwant: %#v", got, want)
	}
}

func requireJSONRequest(t *testing.T, req *http.Request, want map[string]any) {
	t.Helper()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if err := req.Body.Close(); err != nil {
		t.Fatalf("close request body: %v", err)
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))

	requireJSONBody(t, body, want)
}

func stringPtr(value string) *string {
	return &value
}

func slicePtr(values []string) *[]string {
	return &values
}
