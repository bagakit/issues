package issuecore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileSystemStorePersistsCanonicalRecordsForIndexRebuild(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem store: %v", err)
	}
	writeFileStoreManifest(t, store)

	issue := indexFixtureIssue(t, 1, IssueStateOpen, "bagakit/issues", "file backed", "body", []string{"alpha"}, []string{"alice"}, time.Hour)
	writeIssueIndexFixture(t, store, issue)

	reopened, err := NewFileSystemStore(store.Root())
	if err != nil {
		t.Fatalf("reopen filesystem store: %v", err)
	}
	index, err := BuildIssueIndex(ctx, reopened)
	if err != nil {
		t.Fatalf("build index from filesystem store: %v", err)
	}
	page, err := index.List(ListIssuesQuery{Repository: "bagakit/issues"})
	if err != nil {
		t.Fatalf("list rebuilt index: %v", err)
	}
	if len(page.Issues) != 1 || page.Issues[0].ID != issue.ID {
		t.Fatalf("unexpected rebuilt issues: %+v", page.Issues)
	}

	id := IssueID(issue.ID)
	issuePath, err := IssueDocumentPath(id)
	if err != nil {
		t.Fatalf("issue path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), filepath.FromSlash(issuePath.String()))); err != nil {
		t.Fatalf("canonical issue document missing: %v", err)
	}
}

func TestFileSystemStoreHonorsCreateOnlyAndVersionPreconditions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem store: %v", err)
	}
	record := manifestLogicalRecord(t)

	stored, err := store.Write(ctx, record, "", true)
	if err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if stored.Version == "" {
		t.Fatalf("stored record missing version")
	}

	if _, err := store.Write(ctx, record, "", true); !errors.Is(err, ErrLogicalStoreConflict) {
		t.Fatalf("expected create-only conflict, got %v", err)
	}

	updated := record
	updated.Content = append([]byte(nil), record.Content...)
	updated.Content = append(updated.Content, '\n')
	if _, err := store.Write(ctx, updated, RecordVersion("wrong"), false); !errors.Is(err, ErrLogicalStorePreconditionFailed) {
		t.Fatalf("expected precondition failure, got %v", err)
	}
	if _, err := store.Write(ctx, updated, stored.Version, false); err != nil {
		t.Fatalf("write with matching version: %v", err)
	}
}

func TestFileSystemStoreListIgnoresInternalTempFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileSystemStore(root)
	if err != nil {
		t.Fatalf("new filesystem store: %v", err)
	}
	writeFileStoreManifest(t, store)

	tmpDir := filepath.Join(root, "issues")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir temp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".write-leftover.tmp"), []byte("partial"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	entries, _, err := store.List(context.Background(), ListRequest{Prefix: LogicalPath("issues")})
	if err != nil {
		t.Fatalf("list with leftover temp file: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != IssueStoreManifestPath {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func writeFileStoreManifest(t *testing.T, store LogicalStore) {
	t.Helper()

	if _, err := store.Write(context.Background(), manifestLogicalRecord(t), "", true); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func manifestLogicalRecord(t *testing.T) LogicalRecord {
	t.Helper()

	content, err := json.Marshal(DefaultIssueStoreManifest())
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return LogicalRecord{
		Path:          IssueStoreManifestPath,
		MediaType:     "application/json",
		SchemaVersion: IssueStoreManifestSchemaVersion,
		Content:       content,
	}
}
