package issuecore

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMemoryLogicalStoreConformance(t *testing.T) {
	t.Parallel()

	runLogicalStoreConformanceTests(t, func(t *testing.T) LogicalStore {
		t.Helper()
		return newMemLogicalStore()
	})
}

func TestFileSystemLogicalStoreConformance(t *testing.T) {
	t.Parallel()

	runLogicalStoreConformanceTests(t, func(t *testing.T) LogicalStore {
		t.Helper()
		store, err := NewFileSystemStore(t.TempDir())
		if err != nil {
			t.Fatalf("new filesystem store: %v", err)
		}
		return store
	})
}

func runLogicalStoreConformanceTests(t *testing.T, newStore func(*testing.T) LogicalStore) {
	t.Helper()

	t.Run("create_read_list_round_trip", func(t *testing.T) {
		t.Parallel()

		store := newStore(t)
		ctx := context.Background()
		issueID := IssueID(testIssueID)

		issuePath, err := JoinIssuePath(issueID, "issue.md")
		if err != nil {
			t.Fatalf("issue path: %v", err)
		}
		commentPath, err := CommentDocumentPath(issueID, 1)
		if err != nil {
			t.Fatalf("comment path: %v", err)
		}

		records := []LogicalRecord{
			{
				Path:          IssueStoreManifestPath,
				MediaType:     "application/json",
				SchemaVersion: IssueStoreManifestSchemaVersion,
				Content:       []byte(`{"schema_version":"issues.store.manifest.v1"}`),
			},
			{
				Path:          issuePath,
				MediaType:     "text/markdown",
				SchemaVersion: "issues.issue.v1",
				Content:       []byte("# Title\n"),
			},
			{
				Path:          commentPath,
				MediaType:     "text/markdown",
				SchemaVersion: "issues.comment.v1",
				Content:       []byte("first comment"),
			},
		}

		for _, record := range records {
			record := record
			got, err := store.Write(ctx, record, "", true)
			if err != nil {
				t.Fatalf("write %q: %v", record.Path, err)
			}
			if got.Version == "" {
				t.Fatalf("write %q should return a version", record.Path)
			}
		}

		readIssue, err := store.Read(ctx, issuePath)
		if err != nil {
			t.Fatalf("read issue: %v", err)
		}
		if readIssue.Path != issuePath {
			t.Fatalf("read issue path mismatch: got %q want %q", readIssue.Path, issuePath)
		}
		if readIssue.MediaType != "text/markdown" || readIssue.SchemaVersion != "issues.issue.v1" {
			t.Fatalf("unexpected issue metadata: %+v", readIssue)
		}
		if !bytes.Equal(readIssue.Content, []byte("# Title\n")) {
			t.Fatalf("issue content mismatch: %q", readIssue.Content)
		}
		if readIssue.Version == "" {
			t.Fatalf("read issue should include a version")
		}

		issueDir, err := IssueDirectoryPath(issueID)
		if err != nil {
			t.Fatalf("issue dir: %v", err)
		}

		firstPage, cursor, err := store.List(ctx, ListRequest{
			Prefix: issueDir,
			Limit:  1,
		})
		if err != nil {
			t.Fatalf("list first page: %v", err)
		}
		if got, want := len(firstPage), 1; got != want {
			t.Fatalf("unexpected first page length: got %d want %d", got, want)
		}
		if cursor == "" {
			t.Fatalf("expected list cursor for paged results")
		}
		if err := firstPage[0].Validate(); err != nil {
			t.Fatalf("first page entry should validate: %v", err)
		}

		secondPage, nextCursor, err := store.List(ctx, ListRequest{
			Prefix: issueDir,
			Cursor: cursor,
			Limit:  2,
		})
		if err != nil {
			t.Fatalf("list second page: %v", err)
		}
		if nextCursor != "" {
			t.Fatalf("expected second page to exhaust the prefix, got cursor %q", nextCursor)
		}

		paths := []LogicalPath{firstPage[0].Path, secondPage[0].Path}
		wantPaths := []LogicalPath{commentPath, issuePath}
		if !reflect.DeepEqual(paths, wantPaths) {
			t.Fatalf("unexpected logical path order: got %#v want %#v", paths, wantPaths)
		}
	})

	t.Run("read_missing_returns_not_found", func(t *testing.T) {
		t.Parallel()

		store := newStore(t)
		ctx := context.Background()
		path := LogicalPath("issues/by-id/01/8f/" + testIssueID + "/issue.md")

		_, err := store.Read(ctx, path)
		if err == nil {
			t.Fatalf("expected read failure for missing record")
		}
		if !errors.Is(err, ErrLogicalRecordNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("create_only_conflict", func(t *testing.T) {
		t.Parallel()

		store := newStore(t)
		ctx := context.Background()
		path, err := JoinIssuePath(IssueID(testIssueID), "issue.md")
		if err != nil {
			t.Fatalf("issue path: %v", err)
		}

		record := LogicalRecord{
			Path:          path,
			MediaType:     "text/markdown",
			SchemaVersion: "issues.issue.v1",
			Content:       []byte("body"),
		}

		if _, err := store.Write(ctx, record, "", true); err != nil {
			t.Fatalf("initial create: %v", err)
		}
		if _, err := store.Write(ctx, record, "", true); err == nil {
			t.Fatalf("expected create-only conflict")
		} else if !errors.Is(err, ErrLogicalStoreConflict) {
			t.Fatalf("expected conflict error, got %v", err)
		}
	})

	t.Run("versioned_write_uses_precondition_tokens", func(t *testing.T) {
		t.Parallel()

		store := newStore(t)
		ctx := context.Background()
		path, err := JoinIssuePath(IssueID(testIssueID), "issue.md")
		if err != nil {
			t.Fatalf("issue path: %v", err)
		}

		initial, err := store.Write(ctx, LogicalRecord{
			Path:          path,
			MediaType:     "text/markdown",
			SchemaVersion: "issues.issue.v1",
			Content:       []byte("body"),
		}, "", true)
		if err != nil {
			t.Fatalf("initial write: %v", err)
		}

		updated, err := store.Write(ctx, LogicalRecord{
			Path:          path,
			MediaType:     "text/markdown",
			SchemaVersion: "issues.issue.v1",
			Content:       []byte("updated"),
		}, initial.Version, false)
		if err != nil {
			t.Fatalf("versioned update: %v", err)
		}
		if updated.Version == initial.Version {
			t.Fatalf("update should change the record version: got %q", updated.Version)
		}

		_, err = store.Write(ctx, LogicalRecord{
			Path:          path,
			MediaType:     "text/markdown",
			SchemaVersion: "issues.issue.v1",
			Content:       []byte("stale write"),
		}, initial.Version, false)
		if err == nil {
			t.Fatalf("expected precondition failure")
		}
		if !errors.Is(err, ErrLogicalStorePreconditionFailed) {
			t.Fatalf("expected precondition failure, got %v", err)
		}
	})

	t.Run("invalid_paths_are_rejected", func(t *testing.T) {
		t.Parallel()

		store := newStore(t)
		ctx := context.Background()

		if _, err := store.Read(ctx, LogicalPath("/issues/manifest.json")); err == nil {
			t.Fatalf("expected invalid path error from read")
		} else if !errors.Is(err, ErrInvalidLogicalPath) {
			t.Fatalf("expected invalid logical path error, got %v", err)
		}

		_, err := store.Write(ctx, LogicalRecord{
			Path:          LogicalPath("issues//manifest.json"),
			MediaType:     "application/json",
			SchemaVersion: IssueStoreManifestSchemaVersion,
			Content:       []byte("{}"),
		}, "", true)
		if err == nil {
			t.Fatalf("expected invalid path error from write")
		}
		if !errors.Is(err, ErrInvalidLogicalPath) {
			t.Fatalf("expected invalid logical path error, got %v", err)
		}

		if _, _, err := store.List(ctx, ListRequest{Prefix: LogicalPath("issues//bad")}); err == nil {
			t.Fatalf("expected invalid path error from list")
		} else if !errors.Is(err, ErrInvalidLogicalPath) {
			t.Fatalf("expected invalid logical path error, got %v", err)
		}
	})
}
