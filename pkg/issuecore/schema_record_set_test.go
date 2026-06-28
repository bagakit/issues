package issuecore

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const otherTestIssueID = "018f6c57-8c2d-7a60-91ad-8461d3f6be22"

func TestIssueRecordSetRoundTripThroughMemLogicalStore(t *testing.T) {
	t.Parallel()

	issue := schemaFixtureIssue()
	set, err := NewIssueRecordSet(issue)
	if err != nil {
		t.Fatalf("new issue record set: %v", err)
	}

	set.Extensions = append(set.Extensions, ExtensionRecord{
		SchemaVersion: ExtensionRecordSchemaVersion,
		IssueID:       testIssueID,
		Namespace:     "bagakit.meta",
		Data:          json.RawMessage(`{"priority":"high","seen":true}`),
	})

	records, err := set.ToLogicalRecords()
	if err != nil {
		t.Fatalf("logical records: %v", err)
	}

	store := newMemLogicalStore()
	ctx := context.Background()
	for _, record := range records {
		if _, err := store.Write(ctx, record, "", true); err != nil {
			t.Fatalf("write %q: %v", record.Path, err)
		}
	}

	issueDir, err := IssueDirectoryPath(IssueID(testIssueID))
	if err != nil {
		t.Fatalf("issue directory path: %v", err)
	}

	entries, cursor, err := store.List(ctx, ListRequest{Prefix: issueDir})
	if err != nil {
		t.Fatalf("list stored records: %v", err)
	}
	if cursor != "" {
		t.Fatalf("expected issue record listing to fit in one page, got cursor %q", cursor)
	}

	fetched := make([]LogicalRecord, 0, len(entries))
	for _, entry := range entries {
		record, err := store.Read(ctx, entry.Path)
		if err != nil {
			t.Fatalf("read %q: %v", entry.Path, err)
		}
		fetched = append(fetched, record)
	}

	parsed, err := ParseIssueRecordSet(fetched)
	if err != nil {
		t.Fatalf("parse record set: %v", err)
	}

	roundTrippedRecords, err := parsed.ToLogicalRecords()
	if err != nil {
		t.Fatalf("round-trip logical records: %v", err)
	}
	if !reflect.DeepEqual(sortedLogicalRecords(records), sortedLogicalRecords(roundTrippedRecords)) {
		t.Fatalf("logical record round-trip mismatch\n got: %#v\nwant: %#v", sortedLogicalRecords(roundTrippedRecords), sortedLogicalRecords(records))
	}

	gotIssue, err := parsed.ToIssue()
	if err != nil {
		t.Fatalf("round-trip issue: %v", err)
	}
	wantIssue := normalizedSchemaIssue(issue)
	if !reflect.DeepEqual(gotIssue, wantIssue) {
		t.Fatalf("issue round-trip mismatch\n got: %#v\nwant: %#v", gotIssue, wantIssue)
	}
	if gotIssue.Dispatch == nil || gotIssue.Dispatch.Latest == nil || gotIssue.Dispatch.Latest.ID != "dispatch-2" {
		t.Fatalf("dispatch latest should be derived from the highest ordinal record: %#v", gotIssue.Dispatch)
	}
}

func TestParseIssueRecordSetRejectsStrictFrontmatterFailures(t *testing.T) {
	t.Parallel()

	baseRecords := schemaFixtureLogicalRecords(t)
	issuePath := mustIssueDocumentPath(t, IssueID(testIssueID))

	t.Run("unknown_field", func(t *testing.T) {
		t.Parallel()

		records := mustMutateRecord(t, baseRecords, issuePath, func(record *LogicalRecord) {
			record.Content = bytesReplace(record.Content, []byte("\n---\n"), []byte("\nunknown_field: true\n---\n"))
		})

		_, err := ParseIssueRecordSet(records)
		if err == nil {
			t.Fatalf("expected unknown field rejection")
		}
		if !strings.Contains(err.Error(), "unknown_field") {
			t.Fatalf("expected unknown field error, got %v", err)
		}
	})

	t.Run("issue_id_mismatch", func(t *testing.T) {
		t.Parallel()

		records := mustMutateRecord(t, baseRecords, issuePath, func(record *LogicalRecord) {
			record.Content = bytesReplace(record.Content, []byte("id: "+testIssueID), []byte("id: "+otherTestIssueID))
		})

		_, err := ParseIssueRecordSet(records)
		if err == nil {
			t.Fatalf("expected issue id mismatch rejection")
		}
		if !strings.Contains(err.Error(), "does not match path issue id") {
			t.Fatalf("expected path/id mismatch error, got %v", err)
		}
	})

	t.Run("comment_provider_raw_invalid_json", func(t *testing.T) {
		t.Parallel()

		commentPath := mustCommentDocumentPath(t, IssueID(testIssueID), 1)
		records := mustMutateRecord(t, baseRecords, commentPath, func(record *LogicalRecord) {
			record.Content = bytesReplace(record.Content, []byte("provider_raw: '{\"database_id\":101}'"), []byte("provider_raw: '{not-json}'"))
		})

		_, err := ParseIssueRecordSet(records)
		if err == nil {
			t.Fatalf("expected invalid provider_raw rejection")
		}
		if !strings.Contains(err.Error(), "comment document provider_raw") {
			t.Fatalf("expected provider_raw error, got %v", err)
		}
	})

	t.Run("comment_count_mismatch", func(t *testing.T) {
		t.Parallel()

		records := mustMutateRecord(t, baseRecords, issuePath, func(record *LogicalRecord) {
			record.Content = bytesReplace(record.Content, []byte("comments: 2\n"), []byte("comments: 3\n"))
		})

		_, err := ParseIssueRecordSet(records)
		if err == nil {
			t.Fatalf("expected comment count mismatch rejection")
		}
		if !strings.Contains(err.Error(), "issue comment count 3 does not match 2 comment records") {
			t.Fatalf("expected comment count error, got %v", err)
		}
	})
}

func TestParseIssueRecordSetRejectsOrdinalGapsAndDuplicates(t *testing.T) {
	t.Parallel()

	baseRecords := schemaFixtureLogicalRecords(t)
	commentTwoPath := mustCommentDocumentPath(t, IssueID(testIssueID), 2)
	commentThreePath := mustCommentDocumentPath(t, IssueID(testIssueID), 3)
	dispatchTwoPath := mustDispatchRecordPath(t, IssueID(testIssueID), 2)
	dispatchThreePath := mustDispatchRecordPath(t, IssueID(testIssueID), 3)
	timelineTwoPath := mustTimelineEventPath(t, IssueID(testIssueID), 2)

	cases := []struct {
		name    string
		mutate  func(*testing.T, []LogicalRecord) []LogicalRecord
		wantErr string
	}{
		{
			name: "comment_gap",
			mutate: func(t *testing.T, records []LogicalRecord) []LogicalRecord {
				return mustMutateRecord(t, records, commentTwoPath, func(record *LogicalRecord) {
					record.Path = commentThreePath
					record.Content = bytesReplace(record.Content, []byte("ordinal: 2\n"), []byte("ordinal: 3\n"))
				})
			},
			wantErr: "comments: expected ordinal 000002",
		},
		{
			name: "timeline_duplicate_id",
			mutate: func(t *testing.T, records []LogicalRecord) []LogicalRecord {
				return mustMutateRecord(t, records, timelineTwoPath, func(record *LogicalRecord) {
					record.Content = bytesReplace(record.Content, []byte(`"id":"timeline-2"`), []byte(`"id":"timeline-1"`))
				})
			},
			wantErr: `timeline[1]: duplicate id "timeline-1"`,
		},
		{
			name: "dispatch_gap",
			mutate: func(t *testing.T, records []LogicalRecord) []LogicalRecord {
				return mustMutateRecord(t, records, dispatchTwoPath, func(record *LogicalRecord) {
					record.Path = dispatchThreePath
					record.Content = bytesReplace(record.Content, []byte(`"ordinal":2`), []byte(`"ordinal":3`))
				})
			},
			wantErr: "dispatch: expected ordinal 000002",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			records := tc.mutate(t, baseRecords)
			_, err := ParseIssueRecordSet(records)
			if err == nil {
				t.Fatalf("expected validation failure")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q in error, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestProviderIdentityChangesDoNotChangeCanonicalIssuePaths(t *testing.T) {
	t.Parallel()

	issueA := schemaFixtureIssue()
	issueB := schemaFixtureIssue()
	issueB.Repository = "bagakit/issues-renamed"
	issueB.Number = 204
	issueB.URL = "https://api.github.com/repos/bagakit/issues-renamed/issues/204"
	issueB.HTMLURL = "https://github.com/bagakit/issues-renamed/issues/204"
	issueB.NodeID = "issue-node-204"
	refreshDispatchIssueContext(&issueB)

	setA, err := NewIssueRecordSet(issueA)
	if err != nil {
		t.Fatalf("new record set A: %v", err)
	}
	setB, err := NewIssueRecordSet(issueB)
	if err != nil {
		t.Fatalf("new record set B: %v", err)
	}

	recordsA, err := setA.ToLogicalRecords()
	if err != nil {
		t.Fatalf("logical records A: %v", err)
	}
	recordsB, err := setB.ToLogicalRecords()
	if err != nil {
		t.Fatalf("logical records B: %v", err)
	}

	if !reflect.DeepEqual(recordPaths(recordsA), recordPaths(recordsB)) {
		t.Fatalf("provider identity changes should not change canonical paths\n got: %#v\nwant: %#v", recordPaths(recordsB), recordPaths(recordsA))
	}
}

func TestIssueRecordSetUsesPrimaryProviderForIssueProjection(t *testing.T) {
	t.Parallel()

	set, err := NewIssueRecordSet(schemaFixtureIssue())
	if err != nil {
		t.Fatalf("new record set: %v", err)
	}

	set.Providers = append(set.Providers, ProviderIdentityRecord{
		SchemaVersion: ProviderIdentitySchemaVersion,
		IssueID:       testIssueID,
		Provider:      "aaa",
		Repository:    "bagakit/archive",
		Number:        9001,
		URL:           "https://api.github.com/repos/bagakit/archive/issues/9001",
		HTMLURL:       "https://github.com/bagakit/archive/issues/9001",
	})

	got, err := set.ToIssue()
	if err != nil {
		t.Fatalf("to issue: %v", err)
	}
	if got.Provider != "github" || got.Repository != "bagakit/issues" || got.Number != 42 {
		t.Fatalf("expected primary github provider projection, got provider=%q repository=%q number=%d", got.Provider, got.Repository, got.Number)
	}

	set.Issue.PrimaryProvider = ""
	if err := set.Validate(); err == nil {
		t.Fatalf("expected missing primary provider rejection for multi-provider record set")
	}
}

func TestExtensionValidationRejectsInvalidNamespaceAndNonObjectData(t *testing.T) {
	t.Parallel()

	t.Run("invalid_namespace", func(t *testing.T) {
		t.Parallel()

		if _, err := ExtensionPath(IssueID(testIssueID), "Bad.Namespace"); err == nil {
			t.Fatalf("expected invalid namespace error")
		}
	})

	t.Run("non_object_data", func(t *testing.T) {
		t.Parallel()

		record := ExtensionRecord{
			SchemaVersion: ExtensionRecordSchemaVersion,
			IssueID:       testIssueID,
			Namespace:     "bagakit.meta",
			Data:          json.RawMessage(`[1,2,3]`),
		}
		if err := record.Validate(); err == nil {
			t.Fatalf("expected non-object extension data error")
		}
	})
}

func schemaFixtureLogicalRecords(t *testing.T) []LogicalRecord {
	t.Helper()

	set, err := NewIssueRecordSet(schemaFixtureIssue())
	if err != nil {
		t.Fatalf("new issue record set: %v", err)
	}

	records, err := set.ToLogicalRecords()
	if err != nil {
		t.Fatalf("logical records: %v", err)
	}
	return records
}

func schemaFixtureIssue() Issue {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(2 * time.Hour)
	closedAt := createdAt.Add(24 * time.Hour)
	mergedAt := createdAt.Add(3 * time.Hour)
	pinnedAt := createdAt.Add(10 * time.Minute)
	mergeable := true

	issue := Issue{
		ID:          testIssueID,
		Provider:    "github",
		Repository:  "bagakit/issues",
		NodeID:      "issue-node-42",
		Number:      42,
		URL:         "https://api.github.com/repos/bagakit/issues/issues/42",
		HTMLURL:     "https://github.com/bagakit/issues/issues/42",
		Title:       "Schema bundle round trip",
		Body:        "Issue body\n\nwith details.\n",
		BodyText:    "Issue body with details.",
		State:       IssueStateClosed,
		StateReason: IssueStateReasonCompleted,
		User: &Actor{
			ID:      "actor-1",
			NodeID:  "actor-node-1",
			Login:   "alice",
			Type:    "User",
			URL:     "https://api.github.com/users/alice",
			HTMLURL: "https://github.com/alice",
		},
		AuthorAssociation: "OWNER",
		Labels: []Label{
			{ID: "label-1", Name: "core", Color: "ff0000", Description: "Core work"},
		},
		Milestone: &Milestone{
			ID:     "milestone-1",
			Number: 1,
			Title:  "v1",
			State:  IssueStateOpen,
		},
		Assignee: &Actor{ID: "actor-2", Login: "bob"},
		Assignees: []Actor{
			{ID: "actor-2", Login: "bob"},
			{ID: "actor-3", Login: "carol"},
		},
		Comments:         2,
		Locked:           true,
		ActiveLockReason: "resolved",
		Reactions:        &ReactionRollup{TotalCount: 2, Eyes: 1, Rocket: 1},
		CommentItems: []Comment{
			{
				ID:                "comment-1",
				NodeID:            "comment-node-1",
				URL:               "https://api.github.com/repos/bagakit/issues/issues/comments/1",
				HTMLURL:           "https://github.com/bagakit/issues/issues/42#issuecomment-1",
				Body:              "First comment\n",
				BodyText:          "First comment",
				User:              &Actor{ID: "actor-4", Login: "dana"},
				AuthorAssociation: "CONTRIBUTOR",
				CreatedAt:         createdAt.Add(5 * time.Minute),
				UpdatedAt:         createdAt.Add(6 * time.Minute),
				Reactions:         &ReactionRollup{TotalCount: 1, PlusOne: 1},
				Pinned:            true,
				PinnedAt:          &pinnedAt,
				PinnedBy:          &Actor{ID: "actor-1", Login: "alice"},
				ProviderRaw:       json.RawMessage(`{"database_id":101}`),
			},
			{
				ID:                "comment-2",
				NodeID:            "comment-node-2",
				URL:               "https://api.github.com/repos/bagakit/issues/issues/comments/2",
				HTMLURL:           "https://github.com/bagakit/issues/issues/42#issuecomment-2",
				Body:              "Second comment\n",
				BodyText:          "Second comment",
				User:              &Actor{ID: "actor-5", Login: "erin"},
				AuthorAssociation: "MEMBER",
				CreatedAt:         createdAt.Add(7 * time.Minute),
				UpdatedAt:         createdAt.Add(8 * time.Minute),
			},
		},
		Timeline: []TimelineEvent{
			{
				ID:        "timeline-1",
				NodeID:    "timeline-node-1",
				Kind:      "labeled",
				Actor:     &Actor{ID: "actor-1", Login: "alice"},
				CreatedAt: createdAt.Add(2 * time.Minute),
				Payload:   json.RawMessage(`{"label":"core"}`),
			},
			{
				ID:        "timeline-2",
				NodeID:    "timeline-node-2",
				Kind:      "closed",
				Actor:     &Actor{ID: "actor-1", Login: "alice"},
				CreatedAt: closedAt,
				Payload:   json.RawMessage(`{"reason":"completed"}`),
			},
		},
		PullRequest: &PullRequestRef{
			Number:     8,
			ID:         "pr-1",
			Repository: "bagakit/issues",
			HTMLURL:    "https://github.com/bagakit/issues/pull/8",
			State:      PullRequestStateMerged,
			MergedAt:   &mergedAt,
		},
		LinkedPullRequests: []PullRequestRef{
			{
				Number:     9,
				ID:         "pr-2",
				Repository: "bagakit/issues",
				HTMLURL:    "https://github.com/bagakit/issues/pull/9",
				State:      PullRequestStateOpen,
				Mergeable:  &mergeable,
				RequestedReviewers: []Actor{
					{ID: "actor-5", Login: "erin"},
				},
			},
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		ClosedAt:  &closedAt,
		ClosedBy:  &Actor{ID: "actor-1", Login: "alice"},
	}

	contextLink := NewIssueContextLink(issue, ContextFormatPrompt)
	issue.Dispatch = &DispatchMetadata{
		Records: []DispatchRecord{
			{
				ID:          "dispatch-1",
				TargetGroup: DispatchTargetGroup{ID: "ops", Name: "Ops"},
				Terminal: DispatchTerminal{
					Mode: DispatchTerminalModeReuseExisting,
					Existing: &ExistingTerminal{
						ID:               "term-1",
						RuntimePreserved: true,
						RuntimeIdentity:  "codex",
					},
				},
				DispatchedAt: createdAt.Add(15 * time.Minute),
				Outcome:      DispatchOutcomeDelivered,
				IssueContext: contextLink,
			},
			{
				ID:          "dispatch-2",
				TargetGroup: DispatchTargetGroup{ID: "ops", Name: "Ops"},
				Terminal: DispatchTerminal{
					Mode: DispatchTerminalModeCreateNew,
					New: &NewTerminal{
						Title: "Issue 42",
						Runtime: &RuntimeSelection{
							Agent:   "codex",
							Runtime: "go-test",
							Profile: "default",
							Metadata: map[string]string{
								"profile": "default",
							},
						},
					},
				},
				DispatchedAt: createdAt.Add(20 * time.Minute),
				Outcome:      DispatchOutcomePending,
				IssueContext: contextLink,
			},
		},
	}
	latest := issue.Dispatch.Records[0]
	issue.Dispatch.Latest = &latest

	return issue
}

func normalizedSchemaIssue(issue Issue) Issue {
	normalized := issue
	if issue.Dispatch != nil && len(issue.Dispatch.Records) > 0 {
		records := cloneDispatchRecords(issue.Dispatch.Records)
		latest := cloneDispatchRecord(&records[len(records)-1])
		normalized.Dispatch = &DispatchMetadata{
			Records: records,
			Latest:  &latest,
		}
	}
	return normalized
}

func refreshDispatchIssueContext(issue *Issue) {
	if issue == nil || issue.Dispatch == nil {
		return
	}
	for idx := range issue.Dispatch.Records {
		format := issue.Dispatch.Records[idx].IssueContext.Format
		if format == "" {
			format = ContextFormatPrompt
		}
		issue.Dispatch.Records[idx].IssueContext = NewIssueContextLink(*issue, format)
	}
	if issue.Dispatch.Latest != nil {
		format := issue.Dispatch.Latest.IssueContext.Format
		if format == "" {
			format = ContextFormatPrompt
		}
		updated := cloneDispatchRecord(issue.Dispatch.Latest)
		updated.IssueContext = NewIssueContextLink(*issue, format)
		issue.Dispatch.Latest = &updated
	}
}

func sortedLogicalRecords(records []LogicalRecord) []LogicalRecord {
	sorted := cloneLogicalRecords(records)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path.String() < sorted[j].Path.String()
	})
	return sorted
}

func recordPaths(records []LogicalRecord) []string {
	paths := make([]string, 0, len(records))
	for _, record := range sortedLogicalRecords(records) {
		paths = append(paths, record.Path.String())
	}
	return paths
}

func cloneLogicalRecords(records []LogicalRecord) []LogicalRecord {
	cloned := make([]LogicalRecord, len(records))
	for idx, record := range records {
		cloned[idx] = record
		cloned[idx].Content = append([]byte(nil), record.Content...)
	}
	return cloned
}

func mustMutateRecord(t *testing.T, records []LogicalRecord, path LogicalPath, mutate func(*LogicalRecord)) []LogicalRecord {
	t.Helper()

	cloned := cloneLogicalRecords(records)
	for idx := range cloned {
		if cloned[idx].Path == path {
			mutate(&cloned[idx])
			return cloned
		}
	}
	t.Fatalf("record %q not found", path)
	return nil
}

func mustIssueDocumentPath(t *testing.T, issueID IssueID) LogicalPath {
	t.Helper()

	path, err := IssueDocumentPath(issueID)
	if err != nil {
		t.Fatalf("issue document path: %v", err)
	}
	return path
}

func mustCommentDocumentPath(t *testing.T, issueID IssueID, ordinal int) LogicalPath {
	t.Helper()

	path, err := CommentDocumentPath(issueID, ordinal)
	if err != nil {
		t.Fatalf("comment document path: %v", err)
	}
	return path
}

func mustTimelineEventPath(t *testing.T, issueID IssueID, ordinal int) LogicalPath {
	t.Helper()

	path, err := TimelineEventPath(issueID, ordinal)
	if err != nil {
		t.Fatalf("timeline event path: %v", err)
	}
	return path
}

func mustDispatchRecordPath(t *testing.T, issueID IssueID, ordinal int) LogicalPath {
	t.Helper()

	path, err := DispatchRecordPath(issueID, ordinal)
	if err != nil {
		t.Fatalf("dispatch record path: %v", err)
	}
	return path
}

func bytesReplace(content, old, new []byte) []byte {
	return []byte(strings.Replace(string(content), string(old), string(new), 1))
}
