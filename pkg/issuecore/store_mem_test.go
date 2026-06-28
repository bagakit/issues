package issuecore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type memLogicalStore struct {
	mu       sync.Mutex
	records  map[string]LogicalRecord
	versions map[string]int
}

func newMemLogicalStore() *memLogicalStore {
	return &memLogicalStore{
		records:  map[string]LogicalRecord{},
		versions: map[string]int{},
	}
}

func (store *memLogicalStore) Read(ctx context.Context, path LogicalPath) (LogicalRecord, error) {
	if err := ctx.Err(); err != nil {
		return LogicalRecord{}, err
	}
	if err := ValidateLogicalPath(path); err != nil {
		return LogicalRecord{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	record, ok := store.records[path.String()]
	if !ok {
		return LogicalRecord{}, LogicalRecordNotFound(path)
	}
	return cloneLogicalRecord(record), nil
}

func (store *memLogicalStore) Write(ctx context.Context, record LogicalRecord, expect RecordVersion, createOnly bool) (LogicalRecord, error) {
	if err := ctx.Err(); err != nil {
		return LogicalRecord{}, err
	}
	if err := record.Validate(); err != nil {
		return LogicalRecord{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	key := record.Path.String()
	current, exists := store.records[key]
	currentVersion := current.Version

	if createOnly && exists {
		return LogicalRecord{}, LogicalStoreConflict(record.Path, "record already exists")
	}
	if expect != "" && currentVersion != expect {
		return LogicalRecord{}, LogicalStorePreconditionFailed(record.Path, expect, currentVersion)
	}

	store.versions[key]++
	stored := cloneLogicalRecord(record)
	stored.Version = RecordVersion(fmt.Sprintf("v%d", store.versions[key]))
	store.records[key] = stored

	return cloneLogicalRecord(stored), nil
}

func (store *memLogicalStore) List(ctx context.Context, req ListRequest) ([]ListEntry, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if err := req.Validate(); err != nil {
		return nil, "", err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	keys := make([]string, 0, len(store.records))
	for key := range store.records {
		if logicalPathHasPrefix(LogicalPath(key), req.Prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	start := 0
	if req.Cursor != "" {
		for start < len(keys) && keys[start] <= req.Cursor {
			start++
		}
	}

	end := len(keys)
	if req.Limit > 0 && start+req.Limit < end {
		end = start + req.Limit
	}

	entries := make([]ListEntry, 0, end-start)
	for _, key := range keys[start:end] {
		record := store.records[key]
		entries = append(entries, ListEntry{
			Path:          record.Path,
			MediaType:     record.MediaType,
			SchemaVersion: record.SchemaVersion,
			Version:       record.Version,
			SizeBytes:     int64(len(record.Content)),
		})
	}

	var next string
	if end < len(keys) && len(entries) > 0 {
		next = entries[len(entries)-1].Path.String()
	}

	return entries, next, nil
}

func cloneLogicalRecord(record LogicalRecord) LogicalRecord {
	record.Content = append([]byte(nil), record.Content...)
	return record
}

func logicalPathHasPrefix(path, prefix LogicalPath) bool {
	if prefix == "" {
		return true
	}
	raw := path.String()
	head := prefix.String()
	return raw == head || strings.HasPrefix(raw, head+"/")
}
