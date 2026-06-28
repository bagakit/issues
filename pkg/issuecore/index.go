package issuecore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	IssueIndexSnapshotSchemaVersion = "issues.index.snapshot.v1"

	defaultIssueIndexListLimit = 30
	maxIssueIndexListLimit     = 200
	issueIndexStoreListLimit   = 200
)

var (
	ErrIssueIndexCacheMiss    = errors.New("issue index cache miss")
	ErrIssueIndexCacheStale   = errors.New("issue index cache stale")
	ErrIssueIndexCacheCorrupt = errors.New("issue index cache corrupt")
	ErrIssueIndexAmbiguous    = errors.New("issue index ambiguous lookup")
)

type IssueIndexSortField string

const (
	IssueIndexSortNumber  IssueIndexSortField = "number"
	IssueIndexSortCreated IssueIndexSortField = "created"
	IssueIndexSortUpdated IssueIndexSortField = "updated"
)

type IssueIndexSortDirection string

const (
	IssueIndexSortAscending  IssueIndexSortDirection = "asc"
	IssueIndexSortDescending IssueIndexSortDirection = "desc"
)

type IssueIndexQuery struct {
	ListIssuesQuery
	SortBy        IssueIndexSortField     `json:"sort_by,omitempty"`
	SortDirection IssueIndexSortDirection `json:"sort_direction,omitempty"`
}

type ProviderIdentityLocator struct {
	IssueID    string `json:"issue_id,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Repository string `json:"repository,omitempty"`
	Number     int    `json:"number,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	URL        string `json:"url,omitempty"`
	HTMLURL    string `json:"html_url,omitempty"`
}

type IssueIndexEntry struct {
	ID        string                   `json:"id"`
	Issue     Issue                    `json:"issue"`
	Providers []ProviderIdentityRecord `json:"providers,omitempty"`
	Paths     []LogicalPath            `json:"paths,omitempty"`
}

type IssueIndexSnapshot struct {
	SchemaVersion        string            `json:"schema_version"`
	StoreProtocolVersion string            `json:"store_protocol_version"`
	SourceFingerprint    string            `json:"source_fingerprint"`
	Entries              []IssueIndexEntry `json:"entries"`
}

type IssueIndex struct {
	snapshot     IssueIndexSnapshot
	entries      []IssueIndexEntry
	byID         map[string]int
	providerKeys map[string]int
}

type IssueIndexCache interface {
	LoadIssueIndexSnapshot(ctx context.Context) (IssueIndexSnapshot, error)
	StoreIssueIndexSnapshot(ctx context.Context, snapshot IssueIndexSnapshot) error
}

type IssueIndexManager struct {
	Store LogicalStore
	Cache IssueIndexCache
}

func BuildIssueIndex(ctx context.Context, store LogicalStore) (*IssueIndex, error) {
	snapshot, err := BuildIssueIndexSnapshot(ctx, store)
	if err != nil {
		return nil, err
	}
	return NewIssueIndex(snapshot)
}

func BuildIssueIndexSnapshot(ctx context.Context, store LogicalStore) (IssueIndexSnapshot, error) {
	records, fingerprint, protocolVersion, err := collectIssueIndexRecords(ctx, store)
	if err != nil {
		return IssueIndexSnapshot{}, err
	}
	return buildIssueIndexSnapshotFromRecords(records, fingerprint, protocolVersion)
}

func (manager IssueIndexManager) Load(ctx context.Context) (*IssueIndex, error) {
	if manager.Store == nil {
		return nil, fmt.Errorf("issue index store is required")
	}

	records, fingerprint, protocolVersion, err := collectIssueIndexRecords(ctx, manager.Store)
	if err != nil {
		return nil, err
	}

	if manager.Cache != nil {
		if snapshot, err := manager.Cache.LoadIssueIndexSnapshot(ctx); err == nil &&
			snapshot.SourceFingerprint == fingerprint {
			if index, err := NewIssueIndex(snapshot); err == nil {
				return index, nil
			}
		}
	}

	snapshot, err := buildIssueIndexSnapshotFromRecords(records, fingerprint, protocolVersion)
	if err != nil {
		return nil, err
	}
	if manager.Cache != nil {
		_ = manager.Cache.StoreIssueIndexSnapshot(ctx, snapshot)
	}
	return NewIssueIndex(snapshot)
}

func NewIssueIndex(snapshot IssueIndexSnapshot) (*IssueIndex, error) {
	if snapshot.SchemaVersion != IssueIndexSnapshotSchemaVersion {
		return nil, fmt.Errorf("issue index snapshot schema version must be %q", IssueIndexSnapshotSchemaVersion)
	}
	if snapshot.StoreProtocolVersion != IssueStoreProtocolVersion {
		return nil, fmt.Errorf("issue index store protocol version must be %q", IssueStoreProtocolVersion)
	}

	index := &IssueIndex{
		snapshot:     cloneIssueIndexSnapshot(snapshot),
		entries:      make([]IssueIndexEntry, len(snapshot.Entries)),
		byID:         map[string]int{},
		providerKeys: map[string]int{},
	}
	for idx, entry := range snapshot.Entries {
		if _, err := ParseIssueID(entry.ID); err != nil {
			return nil, fmt.Errorf("entries[%d] id: %w", idx, err)
		}
		if err := validateIssueIndexEntry(entry); err != nil {
			return nil, fmt.Errorf("entries[%d]: %w", idx, err)
		}
		if _, exists := index.byID[entry.ID]; exists {
			return nil, fmt.Errorf("entries[%d] duplicate issue id %q", idx, entry.ID)
		}
		index.entries[idx] = cloneIssueIndexEntry(entry)
		index.byID[entry.ID] = idx

		for providerIdx, provider := range entry.Providers {
			if err := provider.Validate(); err != nil {
				return nil, fmt.Errorf("entries[%d] providers[%d]: %w", idx, providerIdx, err)
			}
			if provider.IssueID != entry.ID {
				return nil, fmt.Errorf("entries[%d] providers[%d] issue_id %q does not match entry id %q", idx, providerIdx, provider.IssueID, entry.ID)
			}
			if err := index.addProviderKeys(idx, provider); err != nil {
				return nil, err
			}
		}
	}

	sort.Slice(index.entries, func(i, j int) bool {
		return index.entries[i].ID < index.entries[j].ID
	})
	index.byID = map[string]int{}
	index.providerKeys = map[string]int{}
	for idx, entry := range index.entries {
		index.byID[entry.ID] = idx
		for _, provider := range entry.Providers {
			if err := index.addProviderKeys(idx, provider); err != nil {
				return nil, err
			}
		}
	}

	index.snapshot.Entries = cloneIssueIndexEntries(index.entries)
	return index, nil
}

func (index *IssueIndex) Snapshot() IssueIndexSnapshot {
	if index == nil {
		return IssueIndexSnapshot{}
	}
	return cloneIssueIndexSnapshot(index.snapshot)
}

func (index *IssueIndex) List(query ListIssuesQuery) (IssuePage, error) {
	return index.Query(IssueIndexQuery{ListIssuesQuery: query})
}

func (index *IssueIndex) Query(query IssueIndexQuery) (IssuePage, error) {
	if index == nil {
		return IssuePage{}, fmt.Errorf("issue index is nil")
	}

	state := query.State
	if state == "" {
		state = IssueStateFilterOpen
	}
	switch state {
	case IssueStateFilterOpen, IssueStateFilterClosed, IssueStateFilterAll:
	default:
		return IssuePage{}, fmt.Errorf("unsupported issue state filter %q", query.State)
	}

	limit := query.Limit
	if limit <= 0 {
		limit = defaultIssueIndexListLimit
	}
	if limit > maxIssueIndexListLimit {
		limit = maxIssueIndexListLimit
	}

	start := 0
	if strings.TrimSpace(query.PageToken) != "" {
		offset, err := strconv.Atoi(query.PageToken)
		if err != nil || offset < 0 {
			return IssuePage{}, fmt.Errorf("invalid page token %q", query.PageToken)
		}
		start = offset
	}

	entries := make([]IssueIndexEntry, 0, len(index.entries))
	for _, entry := range index.entries {
		if !issueIndexEntryMatches(entry, query.ListIssuesQuery, state) {
			continue
		}
		entries = append(entries, entry)
	}

	sortBy := query.SortBy
	if sortBy == "" {
		sortBy = IssueIndexSortNumber
	}
	switch sortBy {
	case IssueIndexSortNumber, IssueIndexSortCreated, IssueIndexSortUpdated:
	default:
		return IssuePage{}, fmt.Errorf("unsupported issue index sort field %q", query.SortBy)
	}
	direction := query.SortDirection
	if direction == "" {
		direction = IssueIndexSortDescending
	}
	switch direction {
	case IssueIndexSortAscending, IssueIndexSortDescending:
	default:
		return IssuePage{}, fmt.Errorf("unsupported issue index sort direction %q", query.SortDirection)
	}
	sortIssueIndexEntries(entries, sortBy, direction)

	if start > len(entries) {
		start = len(entries)
	}
	end := start + limit
	if end > len(entries) {
		end = len(entries)
	}

	page := IssuePage{
		Issues: make([]Issue, 0, end-start),
	}
	for _, entry := range entries[start:end] {
		page.Issues = append(page.Issues, cloneIssue(entry.Issue))
	}
	if end < len(entries) {
		page.NextPageToken = strconv.Itoa(end)
	}
	return page, nil
}

func (index *IssueIndex) Get(id IssueID) (Issue, bool) {
	if index == nil {
		return Issue{}, false
	}
	idx, ok := index.byID[id.String()]
	if !ok {
		return Issue{}, false
	}
	return cloneIssue(index.entries[idx].Issue), true
}

func (index *IssueIndex) ResolveProvider(locator ProviderIdentityLocator) (Issue, bool, error) {
	if index == nil {
		return Issue{}, false, fmt.Errorf("issue index is nil")
	}
	if strings.TrimSpace(locator.IssueID) != "" {
		id, err := ParseIssueID(strings.TrimSpace(locator.IssueID))
		if err != nil {
			return Issue{}, false, err
		}
		issue, ok := index.Get(id)
		return issue, ok, nil
	}

	provider := strings.TrimSpace(locator.Provider)
	if provider == "" {
		return Issue{}, false, fmt.Errorf("provider is required for provider identity lookup")
	}
	if err := ValidateProviderToken(provider); err != nil {
		return Issue{}, false, err
	}

	keys := providerLocatorKeys(provider, locator)
	if len(keys) == 0 {
		return Issue{}, false, fmt.Errorf("provider identity locator requires repository+number, external_id, url, or html_url")
	}
	matchedIndex := -1
	for _, key := range keys {
		if idx, ok := index.providerKeys[key]; ok {
			if matchedIndex >= 0 && matchedIndex != idx {
				return Issue{}, false, fmt.Errorf("%w: provider identity locator matched multiple issues", ErrIssueIndexAmbiguous)
			}
			matchedIndex = idx
		}
	}
	if matchedIndex >= 0 {
		return cloneIssue(index.entries[matchedIndex].Issue), true, nil
	}
	return Issue{}, false, nil
}

func buildIssueIndexSnapshotFromRecords(records []LogicalRecord, fingerprint, protocolVersion string) (IssueIndexSnapshot, error) {
	groups := map[string][]LogicalRecord{}
	for _, record := range records {
		parsed, err := parseIssueSchemaPath(record.Path)
		if err != nil {
			return IssueIndexSnapshot{}, err
		}
		groups[parsed.IssueID.String()] = append(groups[parsed.IssueID.String()], record)
	}

	issueIDs := make([]string, 0, len(groups))
	for issueID := range groups {
		issueIDs = append(issueIDs, issueID)
	}
	sort.Strings(issueIDs)

	snapshot := IssueIndexSnapshot{
		SchemaVersion:        IssueIndexSnapshotSchemaVersion,
		StoreProtocolVersion: protocolVersion,
		SourceFingerprint:    fingerprint,
		Entries:              make([]IssueIndexEntry, 0, len(issueIDs)),
	}

	for _, issueID := range issueIDs {
		set, err := ParseIssueRecordSet(groups[issueID])
		if err != nil {
			return IssueIndexSnapshot{}, err
		}
		issue, err := set.ToIssue()
		if err != nil {
			return IssueIndexSnapshot{}, err
		}
		paths := make([]LogicalPath, 0, len(groups[issueID]))
		for _, record := range groups[issueID] {
			paths = append(paths, record.Path)
		}
		sort.Slice(paths, func(i, j int) bool {
			return paths[i].String() < paths[j].String()
		})
		snapshot.Entries = append(snapshot.Entries, IssueIndexEntry{
			ID:        issueID,
			Issue:     issue,
			Providers: cloneProviderIdentityRecords(set.Providers),
			Paths:     append([]LogicalPath(nil), paths...),
		})
	}

	if _, err := NewIssueIndex(snapshot); err != nil {
		return IssueIndexSnapshot{}, err
	}
	return snapshot, nil
}

func collectIssueIndexRecords(ctx context.Context, store LogicalStore) ([]LogicalRecord, string, string, error) {
	if store == nil {
		return nil, "", "", fmt.Errorf("logical store is required")
	}

	manifestRecord, manifest, err := readIssueIndexManifest(ctx, store)
	if err != nil {
		return nil, "", "", err
	}

	var entries []ListEntry
	cursor := ""
	for {
		page, next, err := store.List(ctx, ListRequest{
			Prefix: LogicalPath(IssueStoreByIDPrefix),
			Cursor: cursor,
			Limit:  issueIndexStoreListLimit,
		})
		if err != nil {
			return nil, "", "", err
		}
		entries = append(entries, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path.String() < entries[j].Path.String()
	})

	records := make([]LogicalRecord, 0, len(entries))
	hash := sha256.New()
	writeIssueIndexFingerprint(hash, manifestRecord)
	for idx, entry := range entries {
		if _, err := parseIssueSchemaPath(entry.Path); err != nil {
			return nil, "", "", fmt.Errorf("entries[%d]: %w", idx, err)
		}
		record, err := store.Read(ctx, entry.Path)
		if err != nil {
			return nil, "", "", err
		}
		if record.Path != entry.Path {
			return nil, "", "", fmt.Errorf("logical store read path %q does not match list path %q", record.Path, entry.Path)
		}
		records = append(records, cloneIndexLogicalRecord(record))
		writeIssueIndexFingerprint(hash, record)
	}
	return records, hex.EncodeToString(hash.Sum(nil)), manifest.ProtocolVersion, nil
}

func readIssueIndexManifest(ctx context.Context, store LogicalStore) (LogicalRecord, IssueStoreManifest, error) {
	record, err := store.Read(ctx, IssueStoreManifestPath)
	if err != nil {
		return LogicalRecord{}, IssueStoreManifest{}, err
	}
	if record.MediaType != schemaJSONMediaType {
		return LogicalRecord{}, IssueStoreManifest{}, fmt.Errorf("issue store manifest media type must be %q", schemaJSONMediaType)
	}
	if record.SchemaVersion != IssueStoreManifestSchemaVersion {
		return LogicalRecord{}, IssueStoreManifest{}, fmt.Errorf("issue store manifest logical record schema version must be %q", IssueStoreManifestSchemaVersion)
	}

	var manifest IssueStoreManifest
	if err := parseSchemaJSON(record.Content, &manifest); err != nil {
		return LogicalRecord{}, IssueStoreManifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return LogicalRecord{}, IssueStoreManifest{}, err
	}
	return cloneIndexLogicalRecord(record), manifest, nil
}

type issueIndexHasher interface {
	Write([]byte) (int, error)
}

func writeIssueIndexFingerprint(hash issueIndexHasher, record LogicalRecord) {
	parts := []string{
		record.Path.String(),
		record.MediaType,
		record.SchemaVersion,
		record.Version.String(),
		strconv.Itoa(len(record.Content)),
	}
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	contentHash := sha256.Sum256(record.Content)
	_, _ = hash.Write([]byte(hex.EncodeToString(contentHash[:])))
	_, _ = hash.Write([]byte{0})
}

func validateIssueIndexEntry(entry IssueIndexEntry) error {
	if entry.Issue.ID != entry.ID {
		return fmt.Errorf("issue id %q does not match entry id %q", entry.Issue.ID, entry.ID)
	}
	if strings.TrimSpace(entry.Issue.Title) == "" {
		return fmt.Errorf("issue title is required")
	}
	if err := validateIssueState(entry.Issue.State); err != nil {
		return fmt.Errorf("issue state: %w", err)
	}
	if entry.Issue.CreatedAt.IsZero() {
		return fmt.Errorf("issue created_at is required")
	}
	if entry.Issue.UpdatedAt.IsZero() {
		return fmt.Errorf("issue updated_at is required")
	}
	if entry.Issue.Comments != len(entry.Issue.CommentItems) {
		return fmt.Errorf("issue comment count %d does not match %d comment items", entry.Issue.Comments, len(entry.Issue.CommentItems))
	}
	if len(entry.Providers) == 0 {
		return fmt.Errorf("at least one provider identity record is required")
	}
	return nil
}

func issueIndexEntryMatches(entry IssueIndexEntry, query ListIssuesQuery, state IssueStateFilter) bool {
	issue := entry.Issue
	switch state {
	case IssueStateFilterOpen:
		if issue.State != IssueStateOpen {
			return false
		}
	case IssueStateFilterClosed:
		if issue.State != IssueStateClosed {
			return false
		}
	}
	if repository := strings.TrimSpace(query.Repository); repository != "" && issue.Repository != repository {
		return false
	}
	if assignee := strings.TrimSpace(query.Assignee); assignee != "" && !issueHasAssignee(issue, assignee) {
		return false
	}
	for _, label := range normalizeIndexTokens(query.Labels) {
		if !issueHasLabel(issue, label) {
			return false
		}
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		needle := strings.ToLower(search)
		haystack := strings.ToLower(issue.Title + "\n" + issue.Body + "\n" + issue.BodyText)
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func issueHasAssignee(issue Issue, login string) bool {
	for _, assignee := range issue.Assignees {
		if assignee.Login == login {
			return true
		}
	}
	return issue.Assignee != nil && issue.Assignee.Login == login
}

func issueHasLabel(issue Issue, name string) bool {
	for _, label := range issue.Labels {
		if label.Name == name {
			return true
		}
	}
	return false
}

func normalizeIndexTokens(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortIssueIndexEntries(entries []IssueIndexEntry, field IssueIndexSortField, direction IssueIndexSortDirection) {
	sort.SliceStable(entries, func(i, j int) bool {
		cmp := compareIssueIndexEntries(entries[i], entries[j], field)
		if cmp == 0 {
			cmp = strings.Compare(entries[i].ID, entries[j].ID)
		}
		if direction == IssueIndexSortAscending {
			return cmp < 0
		}
		return cmp > 0
	})
}

func compareIssueIndexEntries(left, right IssueIndexEntry, field IssueIndexSortField) int {
	switch field {
	case IssueIndexSortCreated:
		return compareTime(left.Issue.CreatedAt, right.Issue.CreatedAt)
	case IssueIndexSortUpdated:
		return compareTime(left.Issue.UpdatedAt, right.Issue.UpdatedAt)
	default:
		if left.Issue.Number < right.Issue.Number {
			return -1
		}
		if left.Issue.Number > right.Issue.Number {
			return 1
		}
		return 0
	}
}

func compareTime(left, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func (index *IssueIndex) addProviderKeys(entryIndex int, provider ProviderIdentityRecord) error {
	for _, key := range providerRecordKeys(provider) {
		if existing, exists := index.providerKeys[key]; exists && existing != entryIndex {
			return fmt.Errorf("%w: provider identity key %q maps to both %q and %q", ErrIssueIndexAmbiguous, key, index.entries[existing].ID, index.entries[entryIndex].ID)
		}
		index.providerKeys[key] = entryIndex
	}
	return nil
}

func providerRecordKeys(provider ProviderIdentityRecord) []string {
	return providerLocatorKeys(provider.Provider, ProviderIdentityLocator{
		Repository: provider.Repository,
		Number:     provider.Number,
		ExternalID: provider.ExternalID,
		URL:        provider.URL,
		HTMLURL:    provider.HTMLURL,
	})
}

func providerLocatorKeys(provider string, locator ProviderIdentityLocator) []string {
	provider = strings.TrimSpace(provider)
	keys := []string{}
	if repository := strings.TrimSpace(locator.Repository); repository != "" && locator.Number > 0 {
		keys = append(keys, providerLookupKey("number", provider, repository, strconv.Itoa(locator.Number)))
	}
	if externalID := strings.TrimSpace(locator.ExternalID); externalID != "" {
		keys = append(keys, providerLookupKey("external", provider, externalID))
	}
	if rawURL := strings.TrimSpace(locator.URL); rawURL != "" {
		keys = append(keys, providerLookupKey("url", provider, rawURL))
	}
	if htmlURL := strings.TrimSpace(locator.HTMLURL); htmlURL != "" {
		keys = append(keys, providerLookupKey("html_url", provider, htmlURL))
	}
	return keys
}

func providerLookupKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func cloneIssueIndexSnapshot(snapshot IssueIndexSnapshot) IssueIndexSnapshot {
	snapshot.Entries = cloneIssueIndexEntries(snapshot.Entries)
	return snapshot
}

func cloneIssueIndexEntries(entries []IssueIndexEntry) []IssueIndexEntry {
	cloned := make([]IssueIndexEntry, len(entries))
	for idx, entry := range entries {
		cloned[idx] = cloneIssueIndexEntry(entry)
	}
	return cloned
}

func cloneIssueIndexEntry(entry IssueIndexEntry) IssueIndexEntry {
	entry.Issue = cloneIssue(entry.Issue)
	entry.Providers = cloneProviderIdentityRecords(entry.Providers)
	entry.Paths = append([]LogicalPath(nil), entry.Paths...)
	return entry
}

func cloneIndexLogicalRecord(record LogicalRecord) LogicalRecord {
	record.Content = append([]byte(nil), record.Content...)
	return record
}

func cloneProviderIdentityRecords(records []ProviderIdentityRecord) []ProviderIdentityRecord {
	cloned := make([]ProviderIdentityRecord, len(records))
	for idx, record := range records {
		cloned[idx] = record
		cloned[idx].ProviderRaw = cloneJSONRaw(record.ProviderRaw)
	}
	return cloned
}

func cloneIssue(issue Issue) Issue {
	issue.User = cloneActor(issue.User)
	issue.Labels = cloneLabels(issue.Labels)
	issue.Milestone = cloneMilestone(issue.Milestone)
	issue.Assignee = cloneActor(issue.Assignee)
	issue.Assignees = cloneActors(issue.Assignees)
	issue.Reactions = cloneReactionRollup(issue.Reactions)
	issue.CommentItems = cloneComments(issue.CommentItems)
	issue.Timeline = cloneTimeline(issue.Timeline)
	issue.PullRequest = clonePullRequest(issue.PullRequest)
	issue.LinkedPullRequests = clonePullRequests(issue.LinkedPullRequests)
	issue.Dispatch = cloneDispatchMetadata(issue.Dispatch)
	issue.ClosedAt = cloneTimePtr(issue.ClosedAt)
	issue.ClosedBy = cloneActor(issue.ClosedBy)
	issue.ProviderRaw = cloneJSONRaw(issue.ProviderRaw)
	return issue
}

func cloneComments(comments []Comment) []Comment {
	cloned := make([]Comment, len(comments))
	for idx, comment := range comments {
		cloned[idx] = comment
		cloned[idx].User = cloneActor(comment.User)
		cloned[idx].Reactions = cloneReactionRollup(comment.Reactions)
		cloned[idx].PinnedAt = cloneTimePtr(comment.PinnedAt)
		cloned[idx].PinnedBy = cloneActor(comment.PinnedBy)
		cloned[idx].ProviderRaw = cloneJSONRaw(comment.ProviderRaw)
	}
	return cloned
}

func cloneTimeline(events []TimelineEvent) []TimelineEvent {
	cloned := make([]TimelineEvent, len(events))
	for idx, event := range events {
		cloned[idx] = event
		cloned[idx].Actor = cloneActor(event.Actor)
		cloned[idx].Payload = cloneJSONRaw(event.Payload)
		cloned[idx].ProviderRaw = cloneJSONRaw(event.ProviderRaw)
	}
	return cloned
}
