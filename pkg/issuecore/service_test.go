package issuecore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeProvider struct {
	descriptor       ProviderDescriptor
	listQuery        ListIssuesQuery
	listPage         IssuePage
	getLocator       IssueLocator
	getIssue         Issue
	recordLocator    IssueLocator
	recordDispatches []DispatchRecord
}

func (p *fakeProvider) Descriptor() ProviderDescriptor {
	return p.descriptor
}

func (p *fakeProvider) CreateIssue(context.Context, CreateIssueInput) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeProvider) ListIssues(_ context.Context, query ListIssuesQuery) (IssuePage, error) {
	p.listQuery = query
	return p.listPage, nil
}

func (p *fakeProvider) GetIssue(context.Context, IssueLocator) (Issue, error) {
	return p.getIssue, nil
}

func (p *fakeProvider) UpdateIssue(context.Context, IssueLocator, IssuePatch) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeProvider) AddComment(context.Context, IssueLocator, AddCommentInput) (Comment, error) {
	return Comment{}, nil
}

func (p *fakeProvider) CloseIssue(context.Context, IssueLocator, CloseIssueInput) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeProvider) ReopenIssue(context.Context, IssueLocator, ReopenIssueInput) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeProvider) RecordDispatch(_ context.Context, locator IssueLocator, record DispatchRecord) (Issue, error) {
	p.recordLocator = locator
	p.recordDispatches = append(p.recordDispatches, record)

	issue := p.getIssue
	if issue.Dispatch == nil {
		issue.Dispatch = &DispatchMetadata{}
	}
	issue.Dispatch.Records = append(issue.Dispatch.Records, record)
	latest := record
	issue.Dispatch.Latest = &latest
	return issue, nil
}

type fakeDispatchGateway struct {
	listIssue       Issue
	listTargets     []DispatchTargetGroup
	dispatchIssue   Issue
	dispatchRequest DispatchRequest
	dispatchResult  DispatchResult
}

func (g *fakeDispatchGateway) ListDispatchTargets(_ context.Context, issue Issue) ([]DispatchTargetGroup, error) {
	g.listIssue = issue
	return g.listTargets, nil
}

func (g *fakeDispatchGateway) SubmitDispatch(_ context.Context, issue Issue, request DispatchRequest) (DispatchResult, error) {
	g.dispatchIssue = issue
	g.dispatchRequest = request
	return g.dispatchResult, nil
}

type fakeReadOnlyProvider struct {
	descriptor ProviderDescriptor
	getIssue   Issue
}

func (p *fakeReadOnlyProvider) Descriptor() ProviderDescriptor {
	return p.descriptor
}

func (p *fakeReadOnlyProvider) CreateIssue(context.Context, CreateIssueInput) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeReadOnlyProvider) ListIssues(context.Context, ListIssuesQuery) (IssuePage, error) {
	return IssuePage{}, nil
}

func (p *fakeReadOnlyProvider) GetIssue(context.Context, IssueLocator) (Issue, error) {
	return p.getIssue, nil
}

func (p *fakeReadOnlyProvider) UpdateIssue(context.Context, IssueLocator, IssuePatch) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeReadOnlyProvider) AddComment(context.Context, IssueLocator, AddCommentInput) (Comment, error) {
	return Comment{}, nil
}

func (p *fakeReadOnlyProvider) CloseIssue(context.Context, IssueLocator, CloseIssueInput) (Issue, error) {
	return Issue{}, nil
}

func (p *fakeReadOnlyProvider) ReopenIssue(context.Context, IssueLocator, ReopenIssueInput) (Issue, error) {
	return Issue{}, nil
}

func TestServiceListIssuesNormalizesProviderAndPassesQuery(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.January, 2, 12, 30, 0, 0, time.UTC)
	provider := &fakeProvider{
		descriptor: ProviderDescriptor{
			Name:  ProviderLocal,
			Kind:  ProviderLocal,
			Stage: ProviderStageScaffold,
		},
		listPage: IssuePage{
			Issues: []Issue{
				{
					Repository: "bagakit/issues",
					Number:     1,
					Title:      "wire list",
					State:      IssueStateOpen,
					User:       &Actor{Login: "octocat"},
					CreatedAt:  now,
					UpdatedAt:  now,
				},
			},
		},
	}

	service, err := NewService(provider)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	query := ListIssuesQuery{
		Repository: "bagakit/issues",
		State:      IssueStateFilterOpen,
		Labels:     []string{"bug"},
		Assignee:   "octocat",
		Search:     "wire",
		Limit:      25,
	}

	page, err := service.ListIssues(context.Background(), ProviderLocal, query)
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}

	if !reflect.DeepEqual(provider.listQuery, query) {
		t.Fatalf("query mismatch: got %#v want %#v", provider.listQuery, query)
	}
	if len(page.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(page.Issues))
	}
	if page.Issues[0].Provider != ProviderLocal {
		t.Fatalf("provider should be normalized, got %#v", page.Issues[0].Provider)
	}
}

func TestServiceMissingProviderReturnsLookupError(t *testing.T) {
	t.Parallel()

	service, err := NewService()
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.GetIssue(context.Background(), "missing", IssueLocator{Number: 7})
	if err == nil {
		t.Fatalf("expected lookup error")
	}

	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if opErr.Code != "provider_not_found" {
		t.Fatalf("unexpected error code: %s", opErr.Code)
	}
	if opErr.Provider != "missing" {
		t.Fatalf("unexpected provider: %s", opErr.Provider)
	}
}

func TestServiceListDispatchTargetsUsesGatewayAndNormalizedIssue(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{
		descriptor: ProviderDescriptor{Name: ProviderLocal},
		getIssue: Issue{
			Repository: "bagakit/issues",
			ID:         "local-issue-000007",
			Number:     7,
			Title:      "dispatch me",
			State:      IssueStateOpen,
		},
	}
	gateway := &fakeDispatchGateway{
		listTargets: []DispatchTargetGroup{
			{ID: "grp-1", Name: "Spec"},
			{ID: "grp-2", Name: "Build"},
		},
	}

	service, err := NewServiceWithDispatch(gateway, provider)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	targets, err := service.ListDispatchTargets(context.Background(), ProviderLocal, IssueLocator{Number: 7})
	if err != nil {
		t.Fatalf("list dispatch targets: %v", err)
	}

	if !reflect.DeepEqual(targets, gateway.listTargets) {
		t.Fatalf("dispatch targets mismatch: got %#v want %#v", targets, gateway.listTargets)
	}
	if gateway.listIssue.Provider != ProviderLocal {
		t.Fatalf("gateway should receive normalized provider, got %q", gateway.listIssue.Provider)
	}
	if gateway.listIssue.Number != 7 {
		t.Fatalf("unexpected issue sent to gateway: %+v", gateway.listIssue)
	}
}

func TestServiceSubmitDispatchNormalizesAndPersistsRecord(t *testing.T) {
	t.Parallel()

	dispatchedAt := time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC)
	provider := &fakeProvider{
		descriptor: ProviderDescriptor{Name: ProviderLocal},
		getIssue: Issue{
			Repository: "bagakit/issues",
			ID:         "local-issue-000007",
			Number:     7,
			HTMLURL:    "https://example.invalid/issues/7",
			Title:      "dispatch me",
			State:      IssueStateOpen,
		},
	}
	gateway := &fakeDispatchGateway{
		dispatchResult: DispatchResult{
			Record: DispatchRecord{
				ID:           "dispatch-1",
				DispatchedAt: dispatchedAt,
				Outcome:      DispatchOutcomeDelivered,
			},
		},
	}

	service, err := NewServiceWithDispatch(gateway, provider)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	request := DispatchRequest{
		Issue:       IssueLocator{Number: 7},
		TargetGroup: DispatchTargetGroup{ID: "grp-1", Name: "Spec"},
		Terminal: DispatchTerminal{
			Mode: DispatchTerminalModeCreateNew,
			New: &NewTerminal{
				Title: "Spec Terminal",
			},
		},
	}

	result, err := service.SubmitDispatch(context.Background(), ProviderLocal, request)
	if err != nil {
		t.Fatalf("submit dispatch: %v", err)
	}
	if err := result.Record.Validate(); err != nil {
		t.Fatalf("dispatch result should validate: %v", err)
	}
	if gateway.dispatchIssue.Provider != ProviderLocal {
		t.Fatalf("gateway should receive normalized provider, got %q", gateway.dispatchIssue.Provider)
	}
	if gateway.dispatchRequest.Issue.Provider != ProviderLocal {
		t.Fatalf("request locator provider should be normalized, got %+v", gateway.dispatchRequest.Issue)
	}
	if gateway.dispatchRequest.IssueContext.Provider != ProviderLocal ||
		gateway.dispatchRequest.IssueContext.Repository != "bagakit/issues" ||
		gateway.dispatchRequest.IssueContext.IssueNumber != 7 {
		t.Fatalf("request issue context should be normalized, got %+v", gateway.dispatchRequest.IssueContext)
	}
	if len(provider.recordDispatches) != 1 {
		t.Fatalf("expected one persisted dispatch record, got %d", len(provider.recordDispatches))
	}
	if provider.recordDispatches[0].TargetGroup.ID != "grp-1" {
		t.Fatalf("persisted target group missing: %+v", provider.recordDispatches[0])
	}
	if provider.recordDispatches[0].IssueContext.Provider != ProviderLocal {
		t.Fatalf("persisted issue context missing provider: %+v", provider.recordDispatches[0].IssueContext)
	}
}

func TestServiceSubmitDispatchDoesNotRequireRecorder(t *testing.T) {
	t.Parallel()

	dispatchedAt := time.Date(2024, time.January, 2, 12, 30, 0, 0, time.UTC)
	provider := &fakeReadOnlyProvider{
		descriptor: ProviderDescriptor{Name: ProviderGitHub},
		getIssue: Issue{
			Provider:   ProviderGitHub,
			Repository: "bagakit/issues",
			ID:         "github-issue-7",
			Number:     7,
			HTMLURL:    "https://example.invalid/issues/7",
			Title:      "dispatch github issue",
			State:      IssueStateOpen,
		},
	}
	gateway := &fakeDispatchGateway{
		dispatchResult: DispatchResult{
			Record: DispatchRecord{
				ID:           "dispatch-github-1",
				DispatchedAt: dispatchedAt,
				Outcome:      DispatchOutcomeDelivered,
			},
		},
	}

	service, err := NewServiceWithDispatch(gateway, provider)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.SubmitDispatch(context.Background(), ProviderGitHub, DispatchRequest{
		Issue:       IssueLocator{Number: 7},
		TargetGroup: DispatchTargetGroup{ID: "grp-1", Name: "Spec"},
		Terminal: DispatchTerminal{
			Mode: DispatchTerminalModeReuseExisting,
			Existing: &ExistingTerminal{
				ID:               "term-1",
				RuntimePreserved: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("submit dispatch: %v", err)
	}
	if err := result.Record.Validate(); err != nil {
		t.Fatalf("dispatch result should validate: %v", err)
	}
	if gateway.dispatchIssue.Provider != ProviderGitHub {
		t.Fatalf("gateway should receive github issue, got %+v", gateway.dispatchIssue)
	}
	if gateway.dispatchRequest.IssueContext.Provider != ProviderGitHub {
		t.Fatalf("request issue context should be normalized, got %+v", gateway.dispatchRequest.IssueContext)
	}
}
