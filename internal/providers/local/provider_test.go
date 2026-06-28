package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bagakit/issues/pkg/issuecore"
)

func TestProviderCRUDAndDeterministicExport(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	first, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "first",
		Body:       "alpha body",
		Labels:     []string{"zeta", "alpha"},
		Assignees:  []string{"bob", "alice"},
	})
	if err != nil {
		t.Fatalf("create first issue: %v", err)
	}
	if first.ID != "local-issue-000001" || first.Number != 1 {
		t.Fatalf("unexpected first issue identity: %+v", first)
	}
	if got := labelNames(first.Labels); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("unexpected first issue labels: %#v", got)
	}
	if got := assigneeLogins(first.Assignees); !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Fatalf("unexpected first issue assignees: %#v", got)
	}

	second, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "second",
		Body:       "beta body",
	})
	if err != nil {
		t.Fatalf("create second issue: %v", err)
	}
	if second.ID != "local-issue-000002" || second.Number != 2 {
		t.Fatalf("unexpected second issue identity: %+v", second)
	}

	page, err := provider.ListIssues(ctx, issuecore.ListIssuesQuery{
		Repository: "bagakit/issues",
		State:      issuecore.IssueStateFilterAll,
	})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if got := []int{page.Issues[0].Number, page.Issues[1].Number}; !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("unexpected list order: %#v", got)
	}

	updated, err := provider.UpdateIssue(ctx, issuecore.IssueLocator{Number: 1, Repository: "bagakit/issues"}, issuecore.IssuePatch{
		Title:     stringPtr("first updated"),
		Body:      stringPtr("alpha body updated"),
		Labels:    slicePtr([]string{"beta", "alpha"}),
		Assignees: slicePtr([]string{"alice"}),
	})
	if err != nil {
		t.Fatalf("update issue: %v", err)
	}
	if updated.Title != "first updated" || updated.Body != "alpha body updated" {
		t.Fatalf("unexpected updated issue: %+v", updated)
	}
	if got := labelNames(updated.Labels); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected updated labels: %#v", got)
	}
	if got := assigneeLogins(updated.Assignees); !reflect.DeepEqual(got, []string{"alice"}) {
		t.Fatalf("unexpected updated assignees: %#v", got)
	}

	comment, err := provider.AddComment(ctx, issuecore.IssueLocator{Number: 1, Repository: "bagakit/issues"}, issuecore.AddCommentInput{
		Body: "first comment",
	})
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if comment.ID != "local-comment-000001" {
		t.Fatalf("unexpected comment identity: %+v", comment)
	}

	closed, err := provider.CloseIssue(ctx, issuecore.IssueLocator{Number: 1, Repository: "bagakit/issues"}, issuecore.CloseIssueInput{
		Reason: issuecore.IssueStateReasonNotPlanned,
	})
	if err != nil {
		t.Fatalf("close issue: %v", err)
	}
	if closed.State != issuecore.IssueStateClosed || closed.StateReason != issuecore.IssueStateReasonNotPlanned {
		t.Fatalf("unexpected closed issue: %+v", closed)
	}

	reopened, err := provider.ReopenIssue(ctx, issuecore.IssueLocator{Number: 1, Repository: "bagakit/issues"}, issuecore.ReopenIssueInput{})
	if err != nil {
		t.Fatalf("reopen issue: %v", err)
	}
	if reopened.State != issuecore.IssueStateOpen || reopened.StateReason != issuecore.IssueStateReasonReopened {
		t.Fatalf("unexpected reopened issue: %+v", reopened)
	}

	gotIssue, err := provider.GetIssue(ctx, issuecore.IssueLocator{Number: 1, Repository: "bagakit/issues"})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if gotIssue.Comments != 1 || len(gotIssue.CommentItems) != 1 {
		t.Fatalf("unexpected comment counts: %+v", gotIssue)
	}
	if got := timelineKinds(gotIssue.Timeline); !reflect.DeepEqual(got, []string{"created", "updated", "closed", "reopened"}) {
		t.Fatalf("unexpected timeline kinds: %#v", got)
	}

	filtered, err := provider.ListIssues(ctx, issuecore.ListIssuesQuery{
		Repository: "bagakit/issues",
		State:      issuecore.IssueStateFilterAll,
		Labels:     []string{"alpha", "beta"},
		Assignee:   "alice",
		Search:     "updated",
	})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(filtered.Issues) != 1 || filtered.Issues[0].Number != 1 {
		t.Fatalf("unexpected filtered issues: %+v", filtered.Issues)
	}

	exportOne, err := provider.ExportJSON(ctx)
	if err != nil {
		t.Fatalf("export json first pass: %v", err)
	}
	exportTwo, err := provider.ExportJSON(ctx)
	if err != nil {
		t.Fatalf("export json second pass: %v", err)
	}
	if !bytes.Equal(exportOne, exportTwo) {
		t.Fatalf("export bytes not deterministic\nfirst:\n%s\nsecond:\n%s", exportOne, exportTwo)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(exportOne, &snapshot); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if snapshot.SchemaVersion != schemaVersion() || snapshot.Provider != issuecore.ProviderLocal {
		t.Fatalf("unexpected snapshot header: %+v", snapshot)
	}
	if got := []int{snapshot.Issues[0].Number, snapshot.Issues[1].Number}; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("unexpected export issue order: %#v", got)
	}
	if got := labelNames(snapshot.Issues[0].Labels); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected exported labels: %#v", got)
	}
	if got := assigneeLogins(snapshot.Issues[0].Assignees); !reflect.DeepEqual(got, []string{"alice"}) {
		t.Fatalf("unexpected exported assignees: %#v", got)
	}
	if len(snapshot.Events) != 6 {
		t.Fatalf("unexpected event count: %d", len(snapshot.Events))
	}
	if got := eventKinds(snapshot.Events); !reflect.DeepEqual(got, []string{"created", "created", "updated", "commented", "closed", "reopened"}) {
		t.Fatalf("unexpected event kinds: %#v", got)
	}
	if len(snapshot.ProviderRefs) != 0 {
		t.Fatalf("expected no provider refs, got %+v", snapshot.ProviderRefs)
	}
}

func TestProviderUpdateIssueRejectsEmptyPatchWithoutWrites(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	_, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "first",
		Body:       "alpha body",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	beforeExport, err := provider.ExportJSON(ctx)
	if err != nil {
		t.Fatalf("export before empty update: %v", err)
	}

	var before Snapshot
	if err := json.Unmarshal(beforeExport, &before); err != nil {
		t.Fatalf("decode before export: %v", err)
	}

	_, err = provider.UpdateIssue(ctx, issuecore.IssueLocator{Number: 1, Repository: "bagakit/issues"}, issuecore.IssuePatch{})
	if err == nil {
		t.Fatalf("expected empty patch error")
	}

	var opErr *issuecore.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if opErr.Code != "invalid_argument" {
		t.Fatalf("unexpected error code: %q", opErr.Code)
	}
	if opErr.Provider != issuecore.ProviderLocal {
		t.Fatalf("unexpected provider: %q", opErr.Provider)
	}
	if opErr.Operation != "update" {
		t.Fatalf("unexpected operation: %q", opErr.Operation)
	}

	afterExport, err := provider.ExportJSON(ctx)
	if err != nil {
		t.Fatalf("export after empty update: %v", err)
	}

	var after Snapshot
	if err := json.Unmarshal(afterExport, &after); err != nil {
		t.Fatalf("decode after export: %v", err)
	}

	if len(after.Events) != len(before.Events) {
		t.Fatalf("event count changed: before=%d after=%d", len(before.Events), len(after.Events))
	}
	if !bytes.Equal(beforeExport, afterExport) {
		t.Fatalf("export changed after empty update\nbefore:\n%s\nafter:\n%s", beforeExport, afterExport)
	}
}

func TestProviderRecordDispatchPersistsAcrossGetListAndExport(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "dispatch me",
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
	if updated.Dispatch == nil || updated.Dispatch.Latest == nil {
		t.Fatalf("updated issue missing dispatch metadata: %+v", updated.Dispatch)
	}

	got, err := provider.GetIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if got.Dispatch == nil || got.Dispatch.Latest == nil || got.Dispatch.Latest.ID != "dispatch-1" {
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

	export, err := provider.Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(export.Issues) != 1 || export.Issues[0].Dispatch == nil || export.Issues[0].Dispatch.Latest == nil {
		t.Fatalf("export missing dispatch metadata: %+v", export.Issues)
	}
}

func TestProviderRenderContextIncludesPersistedDispatchMetadata(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "dispatch context",
		Body:       "Context should show dispatch metadata.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	record := issuecore.DispatchRecord{
		ID:          "dispatch-2",
		TargetGroup: issuecore.DispatchTargetGroup{ID: "grp-2", Name: "Build"},
		Terminal: issuecore.DispatchTerminal{
			Mode: issuecore.DispatchTerminalModeCreateNew,
			New: &issuecore.NewTerminal{
				Title: "Build Terminal",
				Runtime: &issuecore.RuntimeSelection{
					Agent:   "codex",
					Runtime: "gpt-5",
				},
			},
		},
		DispatchedAt: time.Date(2024, time.January, 2, 2, 30, 0, 0, time.UTC),
		Outcome:      issuecore.DispatchOutcomeDelivered,
		IssueContext: issuecore.NewIssueContextLink(created, issuecore.ContextFormatJSON),
	}

	if _, err := provider.RecordDispatch(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, record); err != nil {
		t.Fatalf("record dispatch: %v", err)
	}

	got, err := provider.GetIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}

	rendered := issuecore.RenderIssueContext(got, issuecore.DefaultContextOptions())
	if rendered.Issue.Dispatch == nil || rendered.Issue.Dispatch.Latest == nil {
		t.Fatalf("rendered context missing dispatch metadata: %+v", rendered.Issue.Dispatch)
	}
	if err := rendered.Issue.Dispatch.Validate(); err != nil {
		t.Fatalf("rendered dispatch metadata should validate: %v", err)
	}
}

func TestProviderStateReasonValidationRejectsInvalidUpdateCloseAndReopenReasons(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "state reasons",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	updateReason := issuecore.IssueStateReasonCompleted
	_, err = provider.UpdateIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, issuecore.IssuePatch{
		StateReason: &updateReason,
	})
	if err == nil {
		t.Fatalf("expected update state_reason error")
	}
	assertLocalInvalidArgument(t, err, "update", "only accepts state_reason via close or reopen")

	_, err = provider.CloseIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, issuecore.CloseIssueInput{
		Reason: issuecore.IssueStateReasonReopened,
	})
	if err == nil {
		t.Fatalf("expected close reason error")
	}
	assertLocalInvalidArgument(t, err, "close", "unsupported close reason")

	_, err = provider.ReopenIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, issuecore.ReopenIssueInput{
		Reason: issuecore.IssueStateReasonCompleted,
	})
	if err == nil {
		t.Fatalf("expected reopen reason error")
	}
	assertLocalInvalidArgument(t, err, "reopen", "unsupported reopen reason")
}

func TestProviderChangeStateNoOpPreservesIssueMetadataAndTimeline(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "state noop",
		Body:       "do not rewrite",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	reopened, err := provider.ReopenIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, issuecore.ReopenIssueInput{})
	if err != nil {
		t.Fatalf("reopen already-open issue: %v", err)
	}
	if reopened.State != issuecore.IssueStateOpen || reopened.StateReason != created.StateReason {
		t.Fatalf("unexpected reopened issue: %+v", reopened)
	}
	if !reopened.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("updated_at changed on reopen no-op: before=%s after=%s", created.UpdatedAt, reopened.UpdatedAt)
	}
	if reopened.ClosedAt != nil {
		t.Fatalf("closed_at should stay nil on reopen no-op: %+v", reopened.ClosedAt)
	}
	if got := timelineKinds(reopened.Timeline); !reflect.DeepEqual(got, []string{"created"}) {
		t.Fatalf("unexpected timeline after reopen no-op: %#v", got)
	}

	closed, err := provider.CloseIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, issuecore.CloseIssueInput{
		Reason: issuecore.IssueStateReasonNotPlanned,
	})
	if err != nil {
		t.Fatalf("close issue: %v", err)
	}
	if closed.ClosedAt == nil {
		t.Fatalf("closed issue missing closed_at: %+v", closed)
	}

	closedAgain, err := provider.CloseIssue(ctx, issuecore.IssueLocator{Number: created.Number, Repository: created.Repository}, issuecore.CloseIssueInput{
		Reason: issuecore.IssueStateReasonCompleted,
	})
	if err != nil {
		t.Fatalf("close already-closed issue: %v", err)
	}
	if closedAgain.State != issuecore.IssueStateClosed || closedAgain.StateReason != closed.StateReason {
		t.Fatalf("unexpected closed-again issue: %+v", closedAgain)
	}
	if !closedAgain.UpdatedAt.Equal(closed.UpdatedAt) {
		t.Fatalf("updated_at changed on close no-op: before=%s after=%s", closed.UpdatedAt, closedAgain.UpdatedAt)
	}
	if closedAgain.ClosedAt == nil || !closedAgain.ClosedAt.Equal(*closed.ClosedAt) {
		t.Fatalf("closed_at changed on close no-op: before=%v after=%v", closed.ClosedAt, closedAgain.ClosedAt)
	}
	if got := timelineKinds(closedAgain.Timeline); !reflect.DeepEqual(got, []string{"created", "closed"}) {
		t.Fatalf("unexpected timeline after close no-op: %#v", got)
	}
}

func TestProviderListReportsListOperationForLoadFailures(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	ctx := context.Background()

	created, err := provider.CreateIssue(ctx, issuecore.CreateIssueInput{
		Repository: "bagakit/issues",
		Title:      "list failure",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	db, err := provider.ensureDB(ctx, "test", provider.path, provider.now)
	if err != nil {
		t.Fatalf("ensure db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE issues SET created_at = ? WHERE issue_id = ?`, "not-a-timestamp", created.ID); err != nil {
		t.Fatalf("corrupt issue row: %v", err)
	}

	_, err = provider.ListIssues(ctx, issuecore.ListIssuesQuery{
		Repository: created.Repository,
		State:      issuecore.IssueStateFilterAll,
	})
	if err == nil {
		t.Fatalf("expected list failure")
	}

	var opErr *issuecore.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if opErr.Code != "storage_error" {
		t.Fatalf("unexpected error code: %q", opErr.Code)
	}
	if opErr.Operation != "list" {
		t.Fatalf("unexpected operation: %q", opErr.Operation)
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()

	provider, err := New(Config{
		Path: filepath.Join(t.TempDir(), "issues.db"),
		Now:  sequentialClock(time.Date(2024, time.January, 2, 1, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Close()
	})
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

func eventKinds(events []EventRecord) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func assertLocalInvalidArgument(t *testing.T, err error, operation, contains string) {
	t.Helper()

	var opErr *issuecore.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if opErr.Code != "invalid_argument" {
		t.Fatalf("unexpected error code: %q", opErr.Code)
	}
	if opErr.Provider != issuecore.ProviderLocal {
		t.Fatalf("unexpected provider: %q", opErr.Provider)
	}
	if opErr.Operation != operation {
		t.Fatalf("unexpected operation: %q", opErr.Operation)
	}
	if !strings.Contains(opErr.Error(), contains) {
		t.Fatalf("unexpected error: %v", opErr)
	}
}
