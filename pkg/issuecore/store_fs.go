package issuecore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type FileSystemStore struct {
	root string
	mu   sync.Mutex
}

func NewFileSystemStore(root string) (*FileSystemStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("filesystem logical store root is required")
	}
	return &FileSystemStore{root: root}, nil
}

func (store *FileSystemStore) Root() string {
	if store == nil {
		return ""
	}
	return store.root
}

func (store *FileSystemStore) Read(ctx context.Context, path LogicalPath) (LogicalRecord, error) {
	if store == nil {
		return LogicalRecord{}, fmt.Errorf("filesystem logical store is nil")
	}
	if err := ctx.Err(); err != nil {
		return LogicalRecord{}, err
	}
	if err := ValidateLogicalPath(path); err != nil {
		return LogicalRecord{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.readUnlocked(path)
}

func (store *FileSystemStore) Write(ctx context.Context, record LogicalRecord, expect RecordVersion, createOnly bool) (LogicalRecord, error) {
	if store == nil {
		return LogicalRecord{}, fmt.Errorf("filesystem logical store is nil")
	}
	if err := ctx.Err(); err != nil {
		return LogicalRecord{}, err
	}
	if err := record.Validate(); err != nil {
		return LogicalRecord{}, err
	}
	if err := validateLogicalRecordMetadata(record.Path, record.MediaType, record.SchemaVersion); err != nil {
		return LogicalRecord{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	current, err := store.readUnlocked(record.Path)
	switch {
	case err == nil:
		if createOnly {
			return LogicalRecord{}, LogicalStoreConflict(record.Path, "record already exists")
		}
		if expect != "" && current.Version != expect {
			return LogicalRecord{}, LogicalStorePreconditionFailed(record.Path, expect, current.Version)
		}
	case isLogicalRecordNotFound(err):
		if expect != "" {
			return LogicalRecord{}, LogicalStorePreconditionFailed(record.Path, expect, "")
		}
	default:
		return LogicalRecord{}, err
	}

	localPath, err := store.localPath(record.Path)
	if err != nil {
		return LogicalRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return LogicalRecord{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".write-*.tmp")
	if err != nil {
		return LogicalRecord{}, err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(record.Content)
	closeErr := tmp.Close()
	if writeErr != nil {
		_ = os.Remove(tmpName)
		return LogicalRecord{}, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return LogicalRecord{}, closeErr
	}
	if err := os.Rename(tmpName, localPath); err != nil {
		_ = os.Remove(tmpName)
		return LogicalRecord{}, err
	}

	return store.readUnlocked(record.Path)
}

func (store *FileSystemStore) List(ctx context.Context, req ListRequest) ([]ListEntry, string, error) {
	if store == nil {
		return nil, "", fmt.Errorf("filesystem logical store is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if err := req.Validate(); err != nil {
		return nil, "", err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if _, err := os.Stat(store.root); err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}

	entries := []ListEntry{}
	err := filepath.WalkDir(store.root, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".write-") && strings.HasSuffix(entry.Name(), ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(store.root, localPath)
		if err != nil {
			return err
		}
		logical := LogicalPath(filepath.ToSlash(rel))
		if !fsLogicalPathHasPrefix(logical, req.Prefix) {
			return nil
		}
		if err := ValidateLogicalPath(logical); err != nil {
			return err
		}
		mediaType, schemaVersion, err := inferLogicalRecordMetadata(logical)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(localPath)
		if err != nil {
			return err
		}
		entries = append(entries, ListEntry{
			Path:          logical,
			MediaType:     mediaType,
			SchemaVersion: schemaVersion,
			Version:       logicalRecordVersion(logical, mediaType, schemaVersion, content),
			SizeBytes:     int64(len(content)),
		})
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path.String() < entries[j].Path.String()
	})

	start := 0
	if req.Cursor != "" {
		for start < len(entries) && entries[start].Path.String() <= req.Cursor {
			start++
		}
	}

	end := len(entries)
	if req.Limit > 0 && start+req.Limit < end {
		end = start + req.Limit
	}

	page := append([]ListEntry(nil), entries[start:end]...)
	next := ""
	if end < len(entries) && len(page) > 0 {
		next = page[len(page)-1].Path.String()
	}
	return page, next, nil
}

func (store *FileSystemStore) readUnlocked(path LogicalPath) (LogicalRecord, error) {
	localPath, err := store.localPath(path)
	if err != nil {
		return LogicalRecord{}, err
	}
	content, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return LogicalRecord{}, LogicalRecordNotFound(path)
		}
		return LogicalRecord{}, err
	}
	mediaType, schemaVersion, err := inferLogicalRecordMetadata(path)
	if err != nil {
		return LogicalRecord{}, err
	}
	return LogicalRecord{
		Path:          path,
		MediaType:     mediaType,
		SchemaVersion: schemaVersion,
		Content:       content,
		Version:       logicalRecordVersion(path, mediaType, schemaVersion, content),
	}, nil
}

func (store *FileSystemStore) localPath(path LogicalPath) (string, error) {
	if err := ValidateLogicalPath(path); err != nil {
		return "", err
	}
	parts := append([]string{store.root}, strings.Split(path.String(), "/")...)
	return filepath.Join(parts...), nil
}

func inferLogicalRecordMetadata(path LogicalPath) (string, string, error) {
	if path == IssueStoreManifestPath {
		return schemaJSONMediaType, IssueStoreManifestSchemaVersion, nil
	}

	parsed, err := parseIssueSchemaPath(path)
	if err != nil {
		return "", "", err
	}
	switch parsed.Kind {
	case issueSchemaPathIssue:
		return schemaMarkdownMediaType, IssueDocumentSchemaVersion, nil
	case issueSchemaPathComment:
		return schemaMarkdownMediaType, CommentDocumentSchemaVersion, nil
	case issueSchemaPathTimeline:
		return schemaJSONMediaType, TimelineEventSchemaVersion, nil
	case issueSchemaPathProvider:
		return schemaJSONMediaType, ProviderIdentitySchemaVersion, nil
	case issueSchemaPathPullRequests:
		return schemaJSONMediaType, PullRequestLinksSchemaVersion, nil
	case issueSchemaPathDispatch:
		return schemaJSONMediaType, DispatchRecordFileSchemaVersion, nil
	case issueSchemaPathExtension:
		return schemaJSONMediaType, ExtensionRecordSchemaVersion, nil
	default:
		return "", "", invalidLogicalPath(path, "unsupported logical record path")
	}
}

func validateLogicalRecordMetadata(path LogicalPath, mediaType, schemaVersion string) error {
	wantMediaType, wantSchemaVersion, err := inferLogicalRecordMetadata(path)
	if err != nil {
		return err
	}
	if mediaType != wantMediaType {
		return fmt.Errorf("logical record %q media type must be %q", path, wantMediaType)
	}
	if schemaVersion != wantSchemaVersion {
		return fmt.Errorf("logical record %q schema version must be %q", path, wantSchemaVersion)
	}
	return nil
}

func logicalRecordVersion(path LogicalPath, mediaType, schemaVersion string, content []byte) RecordVersion {
	hash := sha256.New()
	for _, part := range []string{path.String(), mediaType, schemaVersion} {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	_, _ = hash.Write(content)
	return RecordVersion("sha256:" + hex.EncodeToString(hash.Sum(nil)))
}

func isLogicalRecordNotFound(err error) bool {
	return errors.Is(err, ErrLogicalRecordNotFound)
}

func fsLogicalPathHasPrefix(path, prefix LogicalPath) bool {
	if prefix == "" {
		return true
	}
	raw := path.String()
	head := prefix.String()
	return raw == head || strings.HasPrefix(raw, head+"/")
}
