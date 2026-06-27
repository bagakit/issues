package issuecore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeProvider struct {
	descriptor ProviderDescriptor
	listQuery  ListIssuesQuery
	listPage   IssuePage
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
	return Issue{}, nil
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
