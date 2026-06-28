package issuecore

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIssueIndexBuildsEmptySnapshotFromManifestOnlyStore(t *testing.T) {
	t.Parallel()

	snapshot, err := BuildIssueIndexSnapshot(context.Background(), newIndexFixtureStore(t))
	if err != nil {
		t.Fatalf("build empty snapshot: %v", err)
	}
	if snapshot.SchemaVersion != IssueIndexSnapshotSchemaVersion {
		t.Fatalf("unexpected schema version: %q", snapshot.SchemaVersion)
	}
	if snapshot.StoreProtocolVersion != IssueStoreProtocolVersion {
		t.Fatalf("unexpected store protocol version: %q", snapshot.StoreProtocolVersion)
	}
	if snapshot.SourceFingerprint == "" {
		t.Fatalf("empty snapshot should still have a source fingerprint")
	}
	if len(snapshot.Entries) != 0 {
		t.Fatalf("expected no entries, got %#v", snapshot.Entries)
	}
}

func TestIssueIndexQueryFiltersSearchSortsAndPaginates(t *testing.T) {
	t.Parallel()

	store := newIndexFixtureStore(t)
	ctx := context.Background()
	first := indexFixtureIssue(t, 1, IssueStateOpen, "bagakit/issues", "first alpha", "alpha body", []string{"alpha", "zeta"}, []string{"alice"}, 1*time.Hour)
	second := indexFixtureIssue(t, 2, IssueStateOpen, "bagakit/issues", "second beta", "beta body", []string{"beta"}, []string{"bob"}, 2*time.Hour)
	third := indexFixtureIssue(t, 3, IssueStateClosed, "bagakit/issues", "third alpha", "closed alpha", []string{"alpha"}, []string{"alice"}, 3*time.Hour)
	writeIssueIndexFixture(t, store, first)
	writeIssueIndexFixture(t, store, second)
	writeIssueIndexFixture(t, store, third)

	index, err := BuildIssueIndex(ctx, store)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}

	page, err := index.List(ListIssuesQuery{Repository: "bagakit/issues"})
	if err != nil {
		t.Fatalf("list default open: %v", err)
	}
	if got := issueNumbers(page.Issues); !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("default list should be open number desc, got %#v", got)
	}

	filtered, err := index.List(ListIssuesQuery{
		Repository: "bagakit/issues",
		State:      IssueStateFilterAll,
		Labels:     []string{"alpha"},
		Assignee:   "alice",
		Search:     "alpha",
	})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if got := issueNumbers(filtered.Issues); !reflect.DeepEqual(got, []int{3, 1}) {
		t.Fatalf("unexpected filtered issues: %#v", got)
	}

	ascending, err := index.Query(IssueIndexQuery{
		ListIssuesQuery: ListIssuesQuery{State: IssueStateFilterAll},
		SortBy:          IssueIndexSortUpdated,
		SortDirection:   IssueIndexSortAscending,
	})
	if err != nil {
		t.Fatalf("updated asc query: %v", err)
	}
	if got := issueNumbers(ascending.Issues); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("unexpected updated asc order: %#v", got)
	}

	firstPage, err := index.Query(IssueIndexQuery{
		ListIssuesQuery: ListIssuesQuery{State: IssueStateFilterAll, Limit: 2},
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if got := issueNumbers(firstPage.Issues); !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("unexpected first page issues: %#v", got)
	}
	if firstPage.NextPageToken == "" {
		t.Fatalf("expected next page token")
	}

	secondPage, err := index.Query(IssueIndexQuery{
		ListIssuesQuery: ListIssuesQuery{State: IssueStateFilterAll, Limit: 2, PageToken: firstPage.NextPageToken},
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got := issueNumbers(secondPage.Issues); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("unexpected second page issues: %#v", got)
	}
}

func TestIssueIndexResolvesProviderIdentityLocators(t *testing.T) {
	t.Parallel()

	store := newIndexFixtureStore(t)
	issue := indexFixtureIssue(t, 42, IssueStateOpen, "bagakit/issues", "lookup", "body", nil, nil, time.Hour)
	set, err := NewIssueRecordSet(issue)
	if err != nil {
		t.Fatalf("new record set: %v", err)
	}
	set.Providers[0].ExternalID = "ext-42"
	records, err := set.ToLogicalRecords()
	if err != nil {
		t.Fatalf("logical records: %v", err)
	}
	writeLogicalRecords(t, store, records)

	index, err := BuildIssueIndex(context.Background(), store)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}

	cases := []ProviderIdentityLocator{
		{IssueID: issue.ID},
		{Provider: ProviderLocal, Repository: "bagakit/issues", Number: 42},
		{Provider: ProviderLocal, ExternalID: "ext-42"},
		{Provider: ProviderLocal, URL: issue.URL},
		{Provider: ProviderLocal, HTMLURL: issue.HTMLURL},
	}
	for _, locator := range cases {
		got, ok, err := index.ResolveProvider(locator)
		if err != nil {
			t.Fatalf("resolve provider locator %#v: %v", locator, err)
		}
		if !ok {
			t.Fatalf("expected locator %#v to resolve", locator)
		}
		if got.ID != issue.ID {
			t.Fatalf("locator %#v resolved issue %q, want %q", locator, got.ID, issue.ID)
		}
	}
}

func TestIssueIndexRejectsAmbiguousProviderLookupKeys(t *testing.T) {
	t.Parallel()

	store := newIndexFixtureStore(t)
	writeIssueIndexFixture(t, store, indexFixtureIssue(t, 7, IssueStateOpen, "bagakit/issues", "first", "body", nil, nil, time.Hour))
	writeIssueIndexFixture(t, store, indexFixtureIssue(t, 7, IssueStateOpen, "bagakit/issues", "second", "body", nil, nil, 2*time.Hour))

	_, err := BuildIssueIndex(context.Background(), store)
	if err == nil {
		t.Fatalf("expected ambiguous provider lookup failure")
	}
	if !errors.Is(err, ErrIssueIndexAmbiguous) {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
}

func TestIssueIndexRejectsConflictingProviderLocatorFields(t *testing.T) {
	t.Parallel()

	store := newIndexFixtureStore(t)
	first := indexFixtureIssue(t, 1, IssueStateOpen, "bagakit/issues", "first", "body", nil, nil, time.Hour)
	second := indexFixtureIssue(t, 2, IssueStateOpen, "bagakit/issues", "second", "body", nil, nil, 2*time.Hour)
	writeIssueIndexFixture(t, store, first)

	set, err := NewIssueRecordSet(second)
	if err != nil {
		t.Fatalf("new second record set: %v", err)
	}
	set.Providers[0].ExternalID = "ext-second"
	records, err := set.ToLogicalRecords()
	if err != nil {
		t.Fatalf("second logical records: %v", err)
	}
	writeLogicalRecords(t, store, records)

	index, err := BuildIssueIndex(context.Background(), store)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}

	_, _, err = index.ResolveProvider(ProviderIdentityLocator{
		Provider:   ProviderLocal,
		Repository: "bagakit/issues",
		Number:     first.Number,
		ExternalID: "ext-second",
	})
	if err == nil {
		t.Fatalf("expected conflicting locator fields to be ambiguous")
	}
	if !errors.Is(err, ErrIssueIndexAmbiguous) {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
}

func TestIssueIndexManagerRebuildsMissingStaleAndCorruptCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newIndexFixtureStore(t)
	cache := &memoryIssueIndexCache{loadErr: ErrIssueIndexCacheMiss}
	writeIssueIndexFixture(t, store, indexFixtureIssue(t, 1, IssueStateOpen, "bagakit/issues", "first", "body", nil, nil, time.Hour))

	manager := IssueIndexManager{Store: store, Cache: cache}
	index, err := manager.Load(ctx)
	if err != nil {
		t.Fatalf("load missing cache: %v", err)
	}
	if got := indexIssueCount(t, index); got != 1 {
		t.Fatalf("unexpected issue count after missing cache rebuild: %d", got)
	}
	if cache.stores != 1 {
		t.Fatalf("expected rebuilt snapshot to be stored once, got %d", cache.stores)
	}

	cache.loadErr = nil
	writeIssueIndexFixture(t, store, indexFixtureIssue(t, 2, IssueStateOpen, "bagakit/issues", "second", "body", nil, nil, 2*time.Hour))
	index, err = manager.Load(ctx)
	if err != nil {
		t.Fatalf("load stale cache: %v", err)
	}
	if got := indexIssueCount(t, index); got != 2 {
		t.Fatalf("unexpected issue count after stale cache rebuild: %d", got)
	}

	cache.snapshot.SchemaVersion = "bad"
	index, err = manager.Load(ctx)
	if err != nil {
		t.Fatalf("load corrupt cache: %v", err)
	}
	if got := indexIssueCount(t, index); got != 2 {
		t.Fatalf("unexpected issue count after corrupt cache rebuild: %d", got)
	}
}

func TestIssueIndexManagerFailsWhenCanonicalRecordsAreInvalid(t *testing.T) {
	t.Parallel()

	store := newIndexFixtureStore(t)
	path, err := IssueDocumentPath(IssueID(testIssueID))
	if err != nil {
		t.Fatalf("issue path: %v", err)
	}
	writeLogicalRecords(t, store, []LogicalRecord{{
		Path:          path,
		MediaType:     schemaMarkdownMediaType,
		SchemaVersion: IssueDocumentSchemaVersion,
		Content:       []byte("---\nschema_version: issues.issue.v1\nid: " + testIssueID + "\ntitle: Broken\nstate: open\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\nbody\n"),
	}})

	cache := &memoryIssueIndexCache{snapshot: IssueIndexSnapshot{SchemaVersion: "bad"}}
	_, err = (IssueIndexManager{Store: store, Cache: cache}).Load(context.Background())
	if err == nil {
		t.Fatalf("expected invalid canonical records to fail rebuild")
	}
	if !strings.Contains(err.Error(), "at least one provider identity record is required") {
		t.Fatalf("expected canonical validation error, got %v", err)
	}
}

func TestIssueIndexBuildFailsWithoutValidManifest(t *testing.T) {
	t.Parallel()

	_, err := BuildIssueIndexSnapshot(context.Background(), newMemLogicalStore())
	if err == nil {
		t.Fatalf("expected missing manifest to fail index rebuild")
	}
	if !errors.Is(err, ErrLogicalRecordNotFound) {
		t.Fatalf("expected manifest not found error, got %v", err)
	}

	store := newMemLogicalStore()
	writeLogicalRecords(t, store, []LogicalRecord{{
		Path:          IssueStoreManifestPath,
		MediaType:     schemaJSONMediaType,
		SchemaVersion: IssueStoreManifestSchemaVersion,
		Content:       []byte(`{"schema_version":"issues.store.manifest.v1","protocol_version":"bad"}`),
	}})
	_, err = BuildIssueIndexSnapshot(context.Background(), store)
	if err == nil {
		t.Fatalf("expected invalid manifest to fail index rebuild")
	}
	if !strings.Contains(err.Error(), "manifest protocol version") {
		t.Fatalf("expected manifest validation error, got %v", err)
	}
}

type memoryIssueIndexCache struct {
	snapshot IssueIndexSnapshot
	loadErr  error
	loads    int
	stores   int
}

func (cache *memoryIssueIndexCache) LoadIssueIndexSnapshot(context.Context) (IssueIndexSnapshot, error) {
	cache.loads++
	if cache.loadErr != nil {
		return IssueIndexSnapshot{}, cache.loadErr
	}
	return cloneIssueIndexSnapshot(cache.snapshot), nil
}

func (cache *memoryIssueIndexCache) StoreIssueIndexSnapshot(_ context.Context, snapshot IssueIndexSnapshot) error {
	cache.stores++
	cache.snapshot = cloneIssueIndexSnapshot(snapshot)
	return nil
}

func newIndexFixtureStore(t *testing.T) *memLogicalStore {
	t.Helper()

	store := newMemLogicalStore()
	writeIssueStoreManifestFixture(t, store)
	return store
}

func writeIssueStoreManifestFixture(t *testing.T, store LogicalStore) {
	t.Helper()

	content, err := marshalSchemaJSON(DefaultIssueStoreManifest())
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	writeLogicalRecords(t, store, []LogicalRecord{{
		Path:          IssueStoreManifestPath,
		MediaType:     schemaJSONMediaType,
		SchemaVersion: IssueStoreManifestSchemaVersion,
		Content:       content,
	}})
}

func indexFixtureIssue(t *testing.T, number int, state IssueState, repository, title, body string, labels, assignees []string, updatedOffset time.Duration) Issue {
	t.Helper()

	id, err := NewIssueID()
	if err != nil {
		t.Fatalf("new issue id: %v", err)
	}
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(updatedOffset)

	issue := Issue{
		Provider:   ProviderLocal,
		Repository: repository,
		ID:         id.String(),
		Number:     number,
		URL:        "https://local.invalid/repos/" + repository + "/issues/" + strconv.Itoa(number),
		HTMLURL:    "https://local.invalid/" + repository + "/issues/" + strconv.Itoa(number),
		Title:      title,
		Body:       body,
		BodyText:   body,
		State:      state,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Comments:   0,
		Labels:     make([]Label, 0, len(labels)),
		Assignees:  make([]Actor, 0, len(assignees)),
	}
	for _, label := range labels {
		issue.Labels = append(issue.Labels, Label{Name: label})
	}
	for _, assignee := range assignees {
		actor := Actor{Login: assignee}
		issue.Assignees = append(issue.Assignees, actor)
		if issue.Assignee == nil {
			issue.Assignee = &actor
		}
	}
	return issue
}

func writeIssueIndexFixture(t *testing.T, store LogicalStore, issue Issue) {
	t.Helper()

	set, err := NewIssueRecordSet(issue)
	if err != nil {
		t.Fatalf("new issue record set: %v", err)
	}
	records, err := set.ToLogicalRecords()
	if err != nil {
		t.Fatalf("logical records: %v", err)
	}
	writeLogicalRecords(t, store, records)
}

func writeLogicalRecords(t *testing.T, store LogicalStore, records []LogicalRecord) {
	t.Helper()

	ctx := context.Background()
	for _, record := range records {
		if _, err := store.Write(ctx, record, "", true); err != nil {
			t.Fatalf("write %q: %v", record.Path, err)
		}
	}
}

func issueNumbers(issues []Issue) []int {
	numbers := make([]int, 0, len(issues))
	for _, issue := range issues {
		numbers = append(numbers, issue.Number)
	}
	return numbers
}

func indexIssueCount(t *testing.T, index *IssueIndex) int {
	t.Helper()

	page, err := index.Query(IssueIndexQuery{ListIssuesQuery: ListIssuesQuery{State: IssueStateFilterAll}})
	if err != nil {
		t.Fatalf("query index count: %v", err)
	}
	return len(page.Issues)
}
