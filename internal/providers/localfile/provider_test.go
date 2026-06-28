package localfile

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bagakit/issues/pkg/issuecore"
)

func TestProviderCRUDUsesCanonicalLogicalFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := newTestProvider(t, root)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "file backed",
		Body:       "body",
		Labels:     []string{"zeta", "alpha"},
		Assignees:  []string{"bob", "alice"},
		Milestone:  "v1",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := issuecore.ValidateIssueID(created.ID); err != nil {
		t.Fatalf("created issue should use canonical issue id: %v", err)
	}
	if created.Number != 1 {
		t.Fatalf("unexpected issue number: %d", created.Number)
	}
	if got := labelNames(created.Labels); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("unexpected labels: %#v", got)
	}
	if got := assigneeLogins(created.Assignees); !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Fatalf("unexpected assignees: %#v", got)
	}
	if created.Milestone == nil || created.Milestone.Title != "v1" {
		t.Fatalf("unexpected milestone: %+v", created.Milestone)
	}

	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(issuecore.IssueStoreManifestPath.String()))); err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
	issueID := issuecore.IssueID(created.ID)
	issuePath, err := issuecore.IssueDocumentPath(issueID)
	if err != nil {
		t.Fatalf("issue path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(issuePath.String()))); err != nil {
		t.Fatalf("canonical issue document was not written: %v", err)
	}
	providerPath, err := issuecore.ProviderIdentityPath(issueID, issuecore.ProviderLocal)
	if err != nil {
		t.Fatalf("provider identity path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(providerPath.String()))); err != nil {
		t.Fatalf("canonical provider identity was not written: %v", err)
	}
	prPath, err := issuecore.PullRequestLinksPath(issueID)
	if err != nil {
		t.Fatalf("pull request links path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(prPath.String()))); err != nil {
		t.Fatalf("canonical pull request links record was not written: %v", err)
	}

	updated, err := provider.UpdateIssue(ctx, issuecore.IssueLocator{Number: 1}, issuecore.IssuePatch{
		Title:     stringPtr("file backed updated"),
		Body:      stringPtr("updated body"),
		Labels:    slicePtr([]string{"beta", "alpha"}),
		Assignees: slicePtr([]string{"alice"}),
		Milestone: stringPtr("v2"),
	})
	if err != nil {
		t.Fatalf("update issue: %v", err)
	}
	if updated.Title != "file backed updated" || updated.Body != "updated body" {
		t.Fatalf("unexpected updated issue: %+v", updated)
	}
	if updated.Milestone == nil || updated.Milestone.Title != "v2" {
		t.Fatalf("unexpected updated milestone: %+v", updated.Milestone)
	}

	comment, err := provider.AddComment(ctx, issuecore.IssueLocator{Number: 1}, issuecore.AddCommentInput{Body: "first comment"})
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if comment.ID == "" || comment.Body != "first comment" {
		t.Fatalf("unexpected comment: %+v", comment)
	}

	closed, err := provider.CloseIssue(ctx, issuecore.IssueLocator{Number: 1}, issuecore.CloseIssueInput{Reason: issuecore.IssueStateReasonNotPlanned})
	if err != nil {
		t.Fatalf("close issue: %v", err)
	}
	if closed.State != issuecore.IssueStateClosed || closed.StateReason != issuecore.IssueStateReasonNotPlanned {
		t.Fatalf("unexpected closed issue: %+v", closed)
	}

	reopened, err := provider.ReopenIssue(ctx, issuecore.IssueLocator{Number: 1}, issuecore.ReopenIssueInput{})
	if err != nil {
		t.Fatalf("reopen issue: %v", err)
	}
	if reopened.State != issuecore.IssueStateOpen || reopened.StateReason != issuecore.IssueStateReasonReopened {
		t.Fatalf("unexpected reopened issue: %+v", reopened)
	}
	if reopened.Comments != 1 || len(reopened.CommentItems) != 1 {
		t.Fatalf("unexpected comments after reopen: %+v", reopened)
	}

	store, err := issuecore.NewFileSystemStore(root)
	if err != nil {
		t.Fatalf("reopen filesystem store: %v", err)
	}
	index, err := issuecore.BuildIssueIndex(ctx, store)
	if err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
	page, err := index.List(issuecore.ListIssuesQuery{State: issuecore.IssueStateFilterAll})
	if err != nil {
		t.Fatalf("list rebuilt index: %v", err)
	}
	if len(page.Issues) != 1 || page.Issues[0].ID != created.ID || page.Issues[0].Comments != 1 {
		t.Fatalf("unexpected rebuilt page: %+v", page.Issues)
	}
}

func TestProviderStateNoOpPreservesTimelineAndUpdatedAt(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, t.TempDir())
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{Title: "noop"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	reopened, err := provider.ReopenIssue(ctx, issuecore.IssueLocator{Number: created.Number}, issuecore.ReopenIssueInput{})
	if err != nil {
		t.Fatalf("reopen open issue: %v", err)
	}
	if !reopened.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("updated_at changed on no-op: before=%s after=%s", created.UpdatedAt, reopened.UpdatedAt)
	}
	if got := timelineKinds(reopened.Timeline); !reflect.DeepEqual(got, []string{"created"}) {
		t.Fatalf("unexpected timeline after no-op: %#v", got)
	}
}

func TestProviderListKeepsNumberCursorPagination(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, t.TempDir())
	ctx := context.Background()

	for _, title := range []string{"first", "second", "third"} {
		if _, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{Title: title, Labels: []string{"alpha"}}); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
	}

	firstPage, err := provider.ListIssues(ctx, issuecore.ListIssuesQuery{
		State:  issuecore.IssueStateFilterAll,
		Labels: []string{"alpha"},
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if got := issueNumbers(firstPage.Issues); !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("unexpected first page: %#v", got)
	}
	if firstPage.NextPageToken != "2" {
		t.Fatalf("next page token should be last issue number, got %q", firstPage.NextPageToken)
	}

	secondPage, err := provider.ListIssues(ctx, issuecore.ListIssuesQuery{
		State:     issuecore.IssueStateFilterAll,
		Labels:    []string{"alpha"},
		Limit:     2,
		PageToken: firstPage.NextPageToken,
	})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if got := issueNumbers(secondPage.Issues); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("unexpected second page: %#v", got)
	}
}

func TestProviderRecordDispatchPersistsCanonicalFileAndContext(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider := newTestProvider(t, root)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "dispatch me",
		Body:       "dispatch body",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	record := issuecore.DispatchRecord{
		ID:          "dispatch-1",
		TargetGroup: issuecore.DispatchTargetGroup{ID: "grp-1", Name: "Spec"},
		Terminal: issuecore.DispatchTerminal{
			Mode: issuecore.DispatchTerminalModeReuseExisting,
			Existing: &issuecore.ExistingTerminal{
				ID:               "term-7",
				Title:            "Worker 7",
				RuntimePreserved: true,
				RuntimeIdentity:  "codex/gpt-5",
			},
		},
		DispatchedAt: time.Date(2024, time.January, 2, 2, 0, 0, 0, time.UTC),
		Outcome:      issuecore.DispatchOutcomeDelivered,
		IssueContext: issuecore.NewIssueContextLink(created, issuecore.ContextFormatPrompt),
	}

	updated, err := provider.RecordDispatch(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, record)
	if err != nil {
		t.Fatalf("record dispatch: %v", err)
	}
	if updated.Dispatch == nil || updated.Dispatch.Latest == nil || updated.Dispatch.Latest.ID != "dispatch-1" {
		t.Fatalf("updated issue missing dispatch metadata: %+v", updated.Dispatch)
	}

	issueID, err := issuecore.ParseIssueID(created.ID)
	if err != nil {
		t.Fatalf("parse issue id: %v", err)
	}
	dispatchPath, err := issuecore.DispatchRecordPath(issueID, 1)
	if err != nil {
		t.Fatalf("dispatch path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dispatchPath.String()))); err != nil {
		t.Fatalf("canonical dispatch record was not written: %v", err)
	}

	got, err := provider.GetIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if got.Dispatch == nil || got.Dispatch.Latest == nil || got.Dispatch.Latest.Terminal.Existing.RuntimeIdentity != "codex/gpt-5" {
		t.Fatalf("get issue missing persisted dispatch: %+v", got.Dispatch)
	}

	page, err := provider.ListIssues(ctx, issuecore.ListIssuesQuery{
		Repository: created.Repository,
		State:      issuecore.IssueStateFilterAll,
	})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if len(page.Issues) != 1 || page.Issues[0].Dispatch == nil || page.Issues[0].Dispatch.Latest == nil {
		t.Fatalf("list issues missing dispatch metadata: %+v", page.Issues)
	}

	store, err := issuecore.NewFileSystemStore(root)
	if err != nil {
		t.Fatalf("reopen filesystem store: %v", err)
	}
	index, err := issuecore.BuildIssueIndex(ctx, store)
	if err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
	indexPage, err := index.List(issuecore.ListIssuesQuery{State: issuecore.IssueStateFilterAll})
	if err != nil {
		t.Fatalf("list rebuilt index: %v", err)
	}
	if len(indexPage.Issues) != 1 || indexPage.Issues[0].Dispatch == nil || indexPage.Issues[0].Dispatch.Latest == nil {
		t.Fatalf("rebuilt index missing dispatch metadata: %+v", indexPage.Issues)
	}

	rendered := issuecore.RenderIssueContext(got, issuecore.DefaultContextOptions())
	if rendered.Issue.Dispatch == nil || rendered.Issue.Dispatch.Latest == nil {
		t.Fatalf("rendered context missing dispatch metadata: %+v", rendered.Issue.Dispatch)
	}
	prompt := issuecore.FormatIssueContextPrompt(rendered)
	for _, want := range []string{
		"Dispatch Records (1):",
		"runtime=codex/gpt-5",
		"context=issues.context.v1/prompt",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func newTestProvider(t *testing.T, root string) *Provider {
	t.Helper()

	provider, err := New(Config{
		Path: root,
		Now:  sequentialClock(time.Date(2024, time.January, 2, 1, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func sequentialClock(base time.Time) func() time.Time {
	var (
		mu      sync.Mutex
		current = base.Add(-time.Second)
	)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		current = current.Add(time.Second)
		return current
	}
}

func stringPtr(value string) *string {
	return &value
}

func slicePtr(values []string) *[]string {
	return &values
}

func labelNames(labels []issuecore.Label) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, label.Name)
	}
	return names
}

func assigneeLogins(assignees []issuecore.Actor) []string {
	logins := make([]string, 0, len(assignees))
	for _, assignee := range assignees {
		logins = append(logins, assignee.Login)
	}
	return logins
}

func timelineKinds(events []issuecore.TimelineEvent) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func issueNumbers(issues []issuecore.Issue) []int {
	numbers := make([]int, 0, len(issues))
	for _, issue := range issues {
		numbers = append(numbers, issue.Number)
	}
	return numbers
}
