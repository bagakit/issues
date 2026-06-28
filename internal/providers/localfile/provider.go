package localfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bagakit/issues/pkg/issuecore"
	"golang.org/x/sys/unix"
)

const (
	EnvStorePath       = "ISSUES_LOCAL_ROOT"
	EnvStorePathCompat = "ISSUES_LOCAL_STORE"
)

const (
	defaultListLimit = 30
	maxListLimit     = 200
)

type Config struct {
	Path  string
	Store issuecore.LogicalStore
	Now   func() time.Time
}

type Provider struct {
	path       string
	store      issuecore.LogicalStore
	now        func() time.Time
	mu         sync.Mutex
	rootMu     *sync.Mutex
	descriptor issuecore.ProviderDescriptor
}

var rootLocks sync.Map

type eventPayload struct {
	Title         string   `json:"title,omitempty"`
	Body          string   `json:"body,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	Assignees     []string `json:"assignees,omitempty"`
	Milestone     string   `json:"milestone,omitempty"`
	ChangedFields []string `json:"changed_fields,omitempty"`
	CommentID     string   `json:"comment_id,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

func New(cfg Config) (*Provider, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	path := strings.TrimSpace(cfg.Path)

	return &Provider{
		path:   path,
		store:  cfg.Store,
		now:    now,
		rootMu: rootLock(path),
		descriptor: issuecore.ProviderDescriptor{
			Name:  issuecore.ProviderLocal,
			Kind:  "logical-files",
			Title: "Local logical file issue provider",
			Stage: issuecore.ProviderStageActive,
			Capabilities: issuecore.CapabilitySet{
				Create:  true,
				List:    true,
				Get:     true,
				Update:  true,
				Comment: true,
				Close:   true,
				Reopen:  true,
			},
		},
	}, nil
}

func (p *Provider) Descriptor() issuecore.ProviderDescriptor {
	return p.descriptor
}

func (p *Provider) Close() error {
	return nil
}

func (p *Provider) CreateIssue(ctx context.Context, input issuecore.CreateIssueInput) (issuecore.Issue, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return issuecore.Issue{}, p.operationError("create", "invalid_argument", errors.New("issue title is required"))
	}

	unlock, err := p.lockMutations(ctx)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	defer unlock()

	store, err := p.ensureStore(ctx, "create")
	if err != nil {
		return issuecore.Issue{}, err
	}

	number, err := p.nextIssueNumber(ctx, store)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	id, err := issuecore.NewIssueID()
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}

	now := p.now().UTC()
	body := normalizeBody(input.Body)
	labels := labelsFromNames(normalizeSet(input.Labels))
	assignees := actorsFromLogins(normalizeSet(input.Assignees))
	issue := issuecore.Issue{
		Provider:   issuecore.ProviderLocal,
		Repository: strings.TrimSpace(input.Repository),
		ID:         id.String(),
		Number:     number,
		URL:        localIssueAPIURL(strings.TrimSpace(input.Repository), number),
		HTMLURL:    localIssueHTMLURL(strings.TrimSpace(input.Repository), number),
		Title:      title,
		Body:       body,
		BodyText:   body,
		State:      issuecore.IssueStateOpen,
		User:       defaultActor(),
		Labels:     labels,
		Milestone:  milestoneFromTitle(input.Milestone),
		Assignees:  assignees,
		Assignee:   firstActor(assignees),
		Comments:   0,
		CreatedAt:  now,
		UpdatedAt:  now,
		Timeline: []issuecore.TimelineEvent{
			newTimelineEvent(number, 1, "created", now, eventPayload{
				Title:     title,
				Body:      body,
				Labels:    normalizeSet(input.Labels),
				Assignees: normalizeSet(input.Assignees),
				Milestone: strings.TrimSpace(input.Milestone),
			}),
		},
	}

	set, err := issuecore.NewIssueRecordSet(issue)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	if err := writeRecordSet(ctx, store, set, true); err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	return set.ToIssue()
}

func (p *Provider) ListIssues(ctx context.Context, query issuecore.ListIssuesQuery) (issuecore.IssuePage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	store, err := p.ensureStore(ctx, "list")
	if err != nil {
		return issuecore.IssuePage{}, err
	}
	index, err := issuecore.BuildIssueIndex(ctx, store)
	if err != nil {
		return issuecore.IssuePage{}, p.operationError("list", "storage_error", err)
	}
	page, err := listByNumberCursor(index, query)
	if err != nil {
		return issuecore.IssuePage{}, p.operationError("list", "invalid_argument", err)
	}
	return page, nil
}

func (p *Provider) GetIssue(ctx context.Context, locator issuecore.IssueLocator) (issuecore.Issue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	store, err := p.ensureStore(ctx, "get")
	if err != nil {
		return issuecore.Issue{}, err
	}
	set, err := p.resolveRecordSet(ctx, store, "get", locator)
	if err != nil {
		return issuecore.Issue{}, err
	}
	return set.ToIssue()
}

func (p *Provider) UpdateIssue(ctx context.Context, locator issuecore.IssueLocator, patch issuecore.IssuePatch) (issuecore.Issue, error) {
	if emptyIssuePatch(patch) {
		return issuecore.Issue{}, p.operationError("update", "invalid_argument", errors.New("issue patch requires at least one field"))
	}
	if patch.StateReason != nil {
		return issuecore.Issue{}, p.operationError("update", "invalid_argument", errors.New("local provider only accepts state_reason via close or reopen"))
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	store, err := p.ensureStore(ctx, "update")
	if err != nil {
		return issuecore.Issue{}, err
	}
	set, err := p.resolveRecordSet(ctx, store, "update", locator)
	if err != nil {
		return issuecore.Issue{}, err
	}

	changedFields := make([]string, 0, 4)
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" {
			return issuecore.Issue{}, p.operationError("update", "invalid_argument", errors.New("issue title cannot be empty"))
		}
		set.Issue.Title = title
		changedFields = append(changedFields, "title")
	}
	if patch.Body != nil {
		body := normalizeBody(*patch.Body)
		set.Issue.Body = body
		set.Issue.BodyText = body
		changedFields = append(changedFields, "body")
	}
	if patch.Labels != nil {
		set.Issue.Labels = labelsFromNames(normalizeSet(*patch.Labels))
		changedFields = append(changedFields, "labels")
	}
	if patch.Assignees != nil {
		assignees := actorsFromLogins(normalizeSet(*patch.Assignees))
		set.Issue.Assignees = assignees
		set.Issue.Assignee = firstActor(assignees)
		changedFields = append(changedFields, "assignees")
	}
	if patch.Milestone != nil {
		set.Issue.Milestone = milestoneFromTitle(*patch.Milestone)
		changedFields = append(changedFields, "milestone")
	}

	now := p.now().UTC()
	number := issueNumberFromSet(set)
	set.Issue.UpdatedAt = now
	set.Timeline = append(set.Timeline, issuecore.TimelineEventRecord{
		SchemaVersion: issuecore.TimelineEventSchemaVersion,
		IssueID:       set.Issue.ID,
		Ordinal:       len(set.Timeline) + 1,
		ID:            localEventID(number, len(set.Timeline)+1),
		Kind:          "updated",
		Actor:         defaultActor(),
		CreatedAt:     now,
		Payload:       mustMarshalPayload(eventPayload{ChangedFields: changedFields}),
	})

	if err := writeRecordSet(ctx, store, set, false); err != nil {
		return issuecore.Issue{}, p.operationError("update", "storage_error", err)
	}
	return set.ToIssue()
}

func (p *Provider) AddComment(ctx context.Context, locator issuecore.IssueLocator, input issuecore.AddCommentInput) (issuecore.Comment, error) {
	body := normalizeBody(input.Body)
	if strings.TrimSpace(body) == "" {
		return issuecore.Comment{}, p.operationError("comment", "invalid_argument", errors.New("comment body is required"))
	}

	unlock, err := p.lockMutations(ctx)
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}
	defer unlock()

	store, err := p.ensureStore(ctx, "comment")
	if err != nil {
		return issuecore.Comment{}, err
	}
	set, err := p.resolveRecordSet(ctx, store, "comment", locator)
	if err != nil {
		return issuecore.Comment{}, err
	}

	now := p.now().UTC()
	number := issueNumberFromSet(set)
	commentOrdinal := len(set.Comments) + 1
	comment := issuecore.CommentDocument{
		SchemaVersion: issuecore.CommentDocumentSchemaVersion,
		IssueID:       set.Issue.ID,
		Ordinal:       commentOrdinal,
		ID:            localCommentID(number, commentOrdinal),
		User:          defaultActor(),
		CreatedAt:     now,
		UpdatedAt:     now,
		Body:          body,
		BodyText:      body,
	}
	set.Comments = append(set.Comments, comment)
	set.Issue.Comments = len(set.Comments)
	set.Issue.UpdatedAt = now
	set.Timeline = append(set.Timeline, issuecore.TimelineEventRecord{
		SchemaVersion: issuecore.TimelineEventSchemaVersion,
		IssueID:       set.Issue.ID,
		Ordinal:       len(set.Timeline) + 1,
		ID:            localEventID(number, len(set.Timeline)+1),
		Kind:          "commented",
		Actor:         defaultActor(),
		CreatedAt:     now,
		Payload:       mustMarshalPayload(eventPayload{CommentID: comment.ID}),
	})

	if err := writeRecordSet(ctx, store, set, false); err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}

	issue, err := set.ToIssue()
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}
	return issue.CommentItems[len(issue.CommentItems)-1], nil
}

func (p *Provider) CloseIssue(ctx context.Context, locator issuecore.IssueLocator, input issuecore.CloseIssueInput) (issuecore.Issue, error) {
	reason, err := issuecore.NormalizeCloseStateReason(input.Reason)
	if err != nil {
		return issuecore.Issue{}, p.operationError("close", "invalid_argument", err)
	}
	return p.changeState(ctx, "close", locator, issuecore.IssueStateClosed, reason)
}

func (p *Provider) ReopenIssue(ctx context.Context, locator issuecore.IssueLocator, input issuecore.ReopenIssueInput) (issuecore.Issue, error) {
	reason, err := issuecore.NormalizeReopenStateReason(input.Reason)
	if err != nil {
		return issuecore.Issue{}, p.operationError("reopen", "invalid_argument", err)
	}
	return p.changeState(ctx, "reopen", locator, issuecore.IssueStateOpen, reason)
}

func (p *Provider) RecordDispatch(ctx context.Context, locator issuecore.IssueLocator, record issuecore.DispatchRecord) (issuecore.Issue, error) {
	if err := record.Validate(); err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "invalid_argument", err)
	}

	unlock, err := p.lockMutations(ctx)
	if err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}
	defer unlock()

	store, err := p.ensureStore(ctx, "dispatch")
	if err != nil {
		return issuecore.Issue{}, err
	}
	set, err := p.resolveRecordSet(ctx, store, "dispatch", locator)
	if err != nil {
		return issuecore.Issue{}, err
	}

	now := p.now().UTC()
	set.Issue.UpdatedAt = now
	set.Dispatch = append(set.Dispatch, issuecore.DispatchRecordFile{
		SchemaVersion: issuecore.DispatchRecordFileSchemaVersion,
		IssueID:       set.Issue.ID,
		Ordinal:       len(set.Dispatch) + 1,
		Record:        record,
	})
	if err := writeRecordSet(ctx, store, set, false); err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}
	return set.ToIssue()
}

func (p *Provider) changeState(ctx context.Context, operation string, locator issuecore.IssueLocator, state issuecore.IssueState, reason issuecore.IssueStateReason) (issuecore.Issue, error) {
	unlock, err := p.lockMutations(ctx)
	if err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}
	defer unlock()

	store, err := p.ensureStore(ctx, operation)
	if err != nil {
		return issuecore.Issue{}, err
	}
	set, err := p.resolveRecordSet(ctx, store, operation, locator)
	if err != nil {
		return issuecore.Issue{}, err
	}
	if set.Issue.State == state {
		return set.ToIssue()
	}

	now := p.now().UTC()
	number := issueNumberFromSet(set)
	set.Issue.State = state
	set.Issue.StateReason = reason
	set.Issue.UpdatedAt = now
	kind := "closed"
	if state == issuecore.IssueStateClosed {
		closedAt := now
		set.Issue.ClosedAt = &closedAt
		set.Issue.ClosedBy = defaultActor()
	} else {
		kind = "reopened"
		set.Issue.ClosedAt = nil
		set.Issue.ClosedBy = nil
	}
	set.Timeline = append(set.Timeline, issuecore.TimelineEventRecord{
		SchemaVersion: issuecore.TimelineEventSchemaVersion,
		IssueID:       set.Issue.ID,
		Ordinal:       len(set.Timeline) + 1,
		ID:            localEventID(number, len(set.Timeline)+1),
		Kind:          kind,
		Actor:         defaultActor(),
		CreatedAt:     now,
		Payload:       mustMarshalPayload(eventPayload{Reason: string(reason)}),
	})

	if err := writeRecordSet(ctx, store, set, false); err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}
	return set.ToIssue()
}

func (p *Provider) ensureStore(ctx context.Context, operation string) (issuecore.LogicalStore, error) {
	if p.store == nil {
		if p.path == "" {
			return nil, p.operationError(operation, "provider_config_error", fmt.Errorf("local provider store path is required (use --store or %s)", EnvStorePath))
		}
		store, err := issuecore.NewFileSystemStore(p.path)
		if err != nil {
			return nil, p.operationError(operation, "provider_config_error", err)
		}
		p.store = store
	}
	if err := ensureManifest(ctx, p.store); err != nil {
		return nil, p.operationError(operation, "storage_error", err)
	}
	return p.store, nil
}

func (p *Provider) lockMutations(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mu := &p.mu
	if p.rootMu != nil {
		mu = p.rootMu
	}
	mu.Lock()

	var lockFile *os.File
	if p.path != "" {
		if err := os.MkdirAll(p.path, 0o755); err != nil {
			mu.Unlock()
			return nil, err
		}
		file, err := os.OpenFile(filepath.Join(p.path, ".issues.lock"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			mu.Unlock()
			return nil, err
		}
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
			_ = file.Close()
			mu.Unlock()
			return nil, err
		}
		lockFile = file
	}

	return func() {
		if lockFile != nil {
			_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
			_ = lockFile.Close()
		}
		mu.Unlock()
	}, nil
}

func rootLock(path string) *sync.Mutex {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	key, err := filepath.Abs(path)
	if err != nil {
		key = filepath.Clean(path)
	}
	value, _ := rootLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func ensureManifest(ctx context.Context, store issuecore.LogicalStore) error {
	record, err := store.Read(ctx, issuecore.IssueStoreManifestPath)
	if err == nil {
		if record.MediaType != "application/json" {
			return fmt.Errorf("issue store manifest media type must be application/json")
		}
		if record.SchemaVersion != issuecore.IssueStoreManifestSchemaVersion {
			return fmt.Errorf("issue store manifest schema version must be %q", issuecore.IssueStoreManifestSchemaVersion)
		}
		var manifest issuecore.IssueStoreManifest
		if err := json.Unmarshal(record.Content, &manifest); err != nil {
			return err
		}
		return manifest.Validate()
	}
	if !errors.Is(err, issuecore.ErrLogicalRecordNotFound) {
		return err
	}

	content, err := json.MarshalIndent(issuecore.DefaultIssueStoreManifest(), "", "  ")
	if err != nil {
		return err
	}
	_, err = store.Write(ctx, issuecore.LogicalRecord{
		Path:          issuecore.IssueStoreManifestPath,
		MediaType:     "application/json",
		SchemaVersion: issuecore.IssueStoreManifestSchemaVersion,
		Content:       content,
	}, "", true)
	return err
}

func (p *Provider) nextIssueNumber(ctx context.Context, store issuecore.LogicalStore) (int, error) {
	snapshot, err := issuecore.BuildIssueIndexSnapshot(ctx, store)
	if err != nil {
		return 0, err
	}
	next := 1
	for _, entry := range snapshot.Entries {
		if entry.Issue.Provider == issuecore.ProviderLocal && entry.Issue.Number >= next {
			next = entry.Issue.Number + 1
		}
	}
	return next, nil
}

func (p *Provider) resolveRecordSet(ctx context.Context, store issuecore.LogicalStore, operation string, locator issuecore.IssueLocator) (issuecore.IssueRecordSet, error) {
	id, err := p.resolveIssueID(ctx, store, operation, locator)
	if err != nil {
		return issuecore.IssueRecordSet{}, err
	}
	return p.loadRecordSet(ctx, store, operation, id)
}

func (p *Provider) resolveIssueID(ctx context.Context, store issuecore.LogicalStore, operation string, locator issuecore.IssueLocator) (issuecore.IssueID, error) {
	if strings.TrimSpace(locator.ID) == "" && locator.Number <= 0 {
		return "", p.operationError(operation, "invalid_argument", errors.New("issue locator requires either number or id"))
	}
	if strings.TrimSpace(locator.ID) != "" {
		id, err := issuecore.ParseIssueID(strings.TrimSpace(locator.ID))
		if err != nil {
			return "", p.operationError(operation, "invalid_argument", err)
		}
		return id, nil
	}
	if strings.TrimSpace(locator.Repository) == "" {
		return p.resolveIssueIDByLocalNumber(ctx, store, operation, locator.Number)
	}

	index, err := issuecore.BuildIssueIndex(ctx, store)
	if err != nil {
		return "", p.operationError(operation, "storage_error", err)
	}
	issue, ok, err := index.ResolveProvider(issuecore.ProviderIdentityLocator{
		Provider:   issuecore.ProviderLocal,
		Repository: strings.TrimSpace(locator.Repository),
		Number:     locator.Number,
	})
	if err != nil {
		return "", p.operationError(operation, "storage_error", err)
	}
	if !ok {
		return "", p.operationError(operation, "not_found", fmt.Errorf("issue %d", locator.Number))
	}
	id, err := issuecore.ParseIssueID(issue.ID)
	if err != nil {
		return "", p.operationError(operation, "storage_error", err)
	}
	return id, nil
}

func (p *Provider) resolveIssueIDByLocalNumber(ctx context.Context, store issuecore.LogicalStore, operation string, number int) (issuecore.IssueID, error) {
	snapshot, err := issuecore.BuildIssueIndexSnapshot(ctx, store)
	if err != nil {
		return "", p.operationError(operation, "storage_error", err)
	}

	match := ""
	for _, entry := range snapshot.Entries {
		if entry.Issue.Provider != issuecore.ProviderLocal || entry.Issue.Number != number {
			continue
		}
		if match != "" && match != entry.Issue.ID {
			return "", p.operationError(operation, "storage_error", fmt.Errorf("local issue number %d matched multiple issues", number))
		}
		match = entry.Issue.ID
	}
	if match == "" {
		return "", p.operationError(operation, "not_found", fmt.Errorf("issue %d", number))
	}
	id, err := issuecore.ParseIssueID(match)
	if err != nil {
		return "", p.operationError(operation, "storage_error", err)
	}
	return id, nil
}

func (p *Provider) loadRecordSet(ctx context.Context, store issuecore.LogicalStore, operation string, id issuecore.IssueID) (issuecore.IssueRecordSet, error) {
	dir, err := issuecore.IssueDirectoryPath(id)
	if err != nil {
		return issuecore.IssueRecordSet{}, p.operationError(operation, "invalid_argument", err)
	}

	records := []issuecore.LogicalRecord{}
	cursor := ""
	for {
		entries, next, err := store.List(ctx, issuecore.ListRequest{Prefix: dir, Cursor: cursor, Limit: 200})
		if err != nil {
			return issuecore.IssueRecordSet{}, p.operationError(operation, "storage_error", err)
		}
		for _, entry := range entries {
			record, err := store.Read(ctx, entry.Path)
			if err != nil {
				return issuecore.IssueRecordSet{}, p.operationError(operation, "storage_error", err)
			}
			records = append(records, record)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(records) == 0 {
		return issuecore.IssueRecordSet{}, p.operationError(operation, "not_found", fmt.Errorf("issue %s", id))
	}
	set, err := issuecore.ParseIssueRecordSet(records)
	if err != nil {
		return issuecore.IssueRecordSet{}, p.operationError(operation, "storage_error", err)
	}
	return set, nil
}

func writeRecordSet(ctx context.Context, store issuecore.LogicalStore, set issuecore.IssueRecordSet, createOnly bool) error {
	records, err := set.ToLogicalRecords()
	if err != nil {
		return err
	}
	for _, record := range commitOrderedRecords(records) {
		expect := issuecore.RecordVersion("")
		writeCreateOnly := createOnly
		if !createOnly {
			current, err := store.Read(ctx, record.Path)
			switch {
			case err == nil:
				expect = current.Version
			case errors.Is(err, issuecore.ErrLogicalRecordNotFound):
				writeCreateOnly = true
			default:
				return err
			}
		}
		if _, err := store.Write(ctx, record, expect, writeCreateOnly); err != nil {
			return err
		}
	}
	return nil
}

func commitOrderedRecords(records []issuecore.LogicalRecord) []issuecore.LogicalRecord {
	ordered := append([]issuecore.LogicalRecord(nil), records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return !isIssueDocumentRecord(ordered[i]) && isIssueDocumentRecord(ordered[j])
	})
	return ordered
}

func isIssueDocumentRecord(record issuecore.LogicalRecord) bool {
	return strings.HasSuffix(record.Path.String(), "/issue.md")
}

func listByNumberCursor(index *issuecore.IssueIndex, query issuecore.ListIssuesQuery) (issuecore.IssuePage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	cursorNumber := 0
	if strings.TrimSpace(query.PageToken) != "" {
		var err error
		cursorNumber, err = parsePositiveInt(query.PageToken)
		if err != nil {
			return issuecore.IssuePage{}, fmt.Errorf("invalid page token %q", query.PageToken)
		}
	}

	results := make([]issuecore.Issue, 0, limit+1)
	indexPageToken := ""
	for {
		indexQuery := query
		indexQuery.PageToken = indexPageToken
		indexQuery.Limit = maxListLimit

		page, err := index.List(indexQuery)
		if err != nil {
			return issuecore.IssuePage{}, err
		}
		for _, issue := range page.Issues {
			if cursorNumber > 0 && issue.Number >= cursorNumber {
				continue
			}
			results = append(results, issue)
			if len(results) > limit {
				break
			}
		}
		if len(results) > limit || page.NextPageToken == "" {
			break
		}
		indexPageToken = page.NextPageToken
	}

	out := issuecore.IssuePage{Issues: results}
	if len(out.Issues) > limit {
		out.NextPageToken = fmt.Sprintf("%d", out.Issues[limit-1].Number)
		out.Issues = out.Issues[:limit]
	}
	return out, nil
}

func parsePositiveInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return 0, err
	}
	if fmt.Sprintf("%d", value) != raw || value <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return value, nil
}

func (p *Provider) operationError(operation, code string, err error) error {
	return &issuecore.OperationError{
		Code:      code,
		Provider:  issuecore.ProviderLocal,
		Operation: operation,
		Err:       err,
	}
}

func emptyIssuePatch(patch issuecore.IssuePatch) bool {
	return patch.Title == nil &&
		patch.Body == nil &&
		patch.Labels == nil &&
		patch.Assignees == nil &&
		patch.Milestone == nil &&
		patch.StateReason == nil
}

func normalizeSet(values []string) []string {
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

func labelsFromNames(names []string) []issuecore.Label {
	labels := make([]issuecore.Label, 0, len(names))
	for _, name := range names {
		labels = append(labels, issuecore.Label{Name: name})
	}
	return labels
}

func actorsFromLogins(logins []string) []issuecore.Actor {
	actors := make([]issuecore.Actor, 0, len(logins))
	for _, login := range logins {
		actors = append(actors, issuecore.Actor{Login: login, Type: "User"})
	}
	return actors
}

func firstActor(actors []issuecore.Actor) *issuecore.Actor {
	if len(actors) == 0 {
		return nil
	}
	actor := actors[0]
	return &actor
}

func milestoneFromTitle(title string) *issuecore.Milestone {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	return &issuecore.Milestone{
		Title: title,
		State: issuecore.IssueStateOpen,
	}
}

func issueNumberFromSet(set issuecore.IssueRecordSet) int {
	provider := providerIdentityForSet(set)
	return provider.Number
}

func providerIdentityForSet(set issuecore.IssueRecordSet) issuecore.ProviderIdentityRecord {
	for _, provider := range set.Providers {
		if provider.Provider == set.Issue.PrimaryProvider {
			return provider
		}
	}
	if len(set.Providers) > 0 {
		return set.Providers[0]
	}
	return issuecore.ProviderIdentityRecord{}
}

func normalizeBody(body string) string {
	return strings.ReplaceAll(body, "\r\n", "\n")
}

func defaultActor() *issuecore.Actor {
	return &issuecore.Actor{
		Login: "local-user",
		Type:  "User",
	}
}

func newTimelineEvent(issueNumber, ordinal int, kind string, createdAt time.Time, payload eventPayload) issuecore.TimelineEvent {
	return issuecore.TimelineEvent{
		ID:        localEventID(issueNumber, ordinal),
		Kind:      kind,
		Actor:     defaultActor(),
		CreatedAt: createdAt,
		Payload:   mustMarshalPayload(payload),
	}
}

func mustMarshalPayload(payload eventPayload) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	if string(raw) == "{}" {
		return nil
	}
	return raw
}

func localCommentID(issueNumber, ordinal int) string {
	return fmt.Sprintf("local-comment-%06d-%06d", issueNumber, ordinal)
}

func localEventID(issueNumber, ordinal int) string {
	return fmt.Sprintf("local-event-%06d-%06d", issueNumber, ordinal)
}

func localIssueAPIURL(repository string, number int) string {
	if repository == "" {
		return fmt.Sprintf("https://local.invalid/issues/%d", number)
	}
	return fmt.Sprintf("https://local.invalid/repos/%s/issues/%d", repository, number)
}

func localIssueHTMLURL(repository string, number int) string {
	if repository == "" {
		return fmt.Sprintf("https://local.invalid/issues/%d", number)
	}
	return fmt.Sprintf("https://local.invalid/%s/issues/%d", repository, number)
}
