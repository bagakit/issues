package local

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/bagakit/issues/pkg/issuecore"
)

const EnvDBPath = "ISSUES_LOCAL_DB"

type Config struct {
	Path string
	Now  func() time.Time
}

type Snapshot struct {
	SchemaVersion int                 `json:"schema_version"`
	Provider      string              `json:"provider"`
	Issues        []issuecore.Issue   `json:"issues"`
	Events        []EventRecord       `json:"events"`
	ProviderRefs  []ProviderRefRecord `json:"provider_refs"`
}

type EventRecord struct {
	Sequence    int64            `json:"sequence"`
	EventID     string           `json:"event_id"`
	IssueID     string           `json:"issue_id"`
	IssueNumber int              `json:"issue_number"`
	Kind        string           `json:"kind"`
	Actor       *issuecore.Actor `json:"actor,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	Payload     json.RawMessage  `json:"payload,omitempty"`
	ProviderRaw json.RawMessage  `json:"provider_raw,omitempty"`
}

type ProviderRefRecord struct {
	IssueID        string          `json:"issue_id"`
	Provider       string          `json:"provider"`
	ExternalID     string          `json:"external_id,omitempty"`
	ExternalNodeID string          `json:"external_node_id,omitempty"`
	URL            string          `json:"url,omitempty"`
	HTMLURL        string          `json:"html_url,omitempty"`
	ETag           string          `json:"etag,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

type Provider struct {
	path string
	now  func() time.Time
	dbHandle
	descriptor issuecore.ProviderDescriptor
}

type issueKey struct {
	IssueID string
	Number  int
}

type eventPayload struct {
	Title         string   `json:"title,omitempty"`
	Body          string   `json:"body,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	Assignees     []string `json:"assignees,omitempty"`
	ChangedFields []string `json:"changed_fields,omitempty"`
	CommentID     string   `json:"comment_id,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

func New(cfg Config) (*Provider, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Provider{
		path: cfg.Path,
		now:  now,
		descriptor: issuecore.ProviderDescriptor{
			Name:  issuecore.ProviderLocal,
			Kind:  "sqlite",
			Title: "Local SQLite issue provider",
			Stage: "ready",
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
	return p.closeDB()
}

func (p *Provider) CreateIssue(ctx context.Context, input issuecore.CreateIssueInput) (issuecore.Issue, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return issuecore.Issue{}, p.operationError("create", "invalid_argument", errors.New("issue title is required"))
	}

	db, err := p.ensureDB(ctx, "create", p.path, p.now)
	if err != nil {
		return issuecore.Issue{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	defer tx.Rollback()

	now := p.now().UTC()
	issueNumber, err := nextCounter(ctx, tx, counterIssues)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}

	issueID := formatIssueID(issueNumber)
	author := defaultActor()
	body := normalizeBody(input.Body)
	labels := normalizeSet(input.Labels)
	assignees := normalizeSet(input.Assignees)

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO issues (
			issue_id, provider, repository, number, title, body_markdown, body_text,
			state, author_login, author_type, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issueID,
		issuecore.ProviderLocal,
		strings.TrimSpace(input.Repository),
		issueNumber,
		title,
		body,
		body,
		string(issuecore.IssueStateOpen),
		author.Login,
		author.Type,
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}

	if err := replaceLabels(ctx, tx, issueID, labels); err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	if err := replaceAssignees(ctx, tx, issueID, assignees); err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}

	payload, err := marshalJSON(eventPayload{
		Title:     title,
		Body:      body,
		Labels:    labels,
		Assignees: assignees,
	})
	if err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}
	if err := p.appendEventTx(ctx, tx, issueKey{IssueID: issueID, Number: int(issueNumber)}, "created", author, now, payload); err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}

	issue, err := p.loadIssueByID(ctx, tx, issueID, true)
	if err != nil {
		return issuecore.Issue{}, err
	}

	if err := tx.Commit(); err != nil {
		return issuecore.Issue{}, p.operationError("create", "storage_error", err)
	}

	return issue, nil
}

func (p *Provider) ListIssues(ctx context.Context, query issuecore.ListIssuesQuery) (issuecore.IssuePage, error) {
	db, err := p.ensureDB(ctx, "list", p.path, p.now)
	if err != nil {
		return issuecore.IssuePage{}, err
	}
	return p.listIssues(ctx, db, query)
}

func (p *Provider) GetIssue(ctx context.Context, locator issuecore.IssueLocator) (issuecore.Issue, error) {
	db, err := p.ensureDB(ctx, "get", p.path, p.now)
	if err != nil {
		return issuecore.Issue{}, err
	}

	key, err := p.resolveIssue(ctx, db, "get", locator)
	if err != nil {
		return issuecore.Issue{}, err
	}
	return p.loadIssueByID(ctx, db, key.IssueID, true)
}

func (p *Provider) UpdateIssue(ctx context.Context, locator issuecore.IssueLocator, patch issuecore.IssuePatch) (issuecore.Issue, error) {
	if emptyIssuePatch(patch) {
		return issuecore.Issue{}, p.operationError("update", "invalid_argument", errors.New("issue patch requires at least one field"))
	}
	if patch.StateReason != nil {
		return issuecore.Issue{}, p.operationError("update", "invalid_argument", errors.New("local provider only accepts state_reason via close or reopen"))
	}

	db, err := p.ensureDB(ctx, "update", p.path, p.now)
	if err != nil {
		return issuecore.Issue{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return issuecore.Issue{}, p.operationError("update", "storage_error", err)
	}
	defer tx.Rollback()

	key, err := p.resolveIssue(ctx, tx, "update", locator)
	if err != nil {
		return issuecore.Issue{}, err
	}

	record, err := p.loadIssueRecord(ctx, tx, key.IssueID)
	if err != nil {
		return issuecore.Issue{}, err
	}

	title := record.Title
	body := record.Body
	changedFields := make([]string, 0, 4)

	if patch.Title != nil {
		title = strings.TrimSpace(*patch.Title)
		if title == "" {
			return issuecore.Issue{}, p.operationError("update", "invalid_argument", errors.New("issue title cannot be empty"))
		}
		changedFields = append(changedFields, "title")
	}
	if patch.Body != nil {
		body = normalizeBody(*patch.Body)
		changedFields = append(changedFields, "body")
	}

	now := p.now().UTC()
	_, err = tx.ExecContext(
		ctx,
		`UPDATE issues
		 SET title = ?, body_markdown = ?, body_text = ?, state_reason = NULLIF(?, ''), updated_at = ?
		 WHERE issue_id = ?`,
		title,
		body,
		body,
		record.StateReason,
		formatTime(now),
		key.IssueID,
	)
	if err != nil {
		return issuecore.Issue{}, p.operationError("update", "storage_error", err)
	}

	var labels []string
	if patch.Labels != nil {
		labels = normalizeSet(*patch.Labels)
		if err := replaceLabels(ctx, tx, key.IssueID, labels); err != nil {
			return issuecore.Issue{}, p.operationError("update", "storage_error", err)
		}
		changedFields = append(changedFields, "labels")
	}

	var assignees []string
	if patch.Assignees != nil {
		assignees = normalizeSet(*patch.Assignees)
		if err := replaceAssignees(ctx, tx, key.IssueID, assignees); err != nil {
			return issuecore.Issue{}, p.operationError("update", "storage_error", err)
		}
		changedFields = append(changedFields, "assignees")
	}

	payload, err := marshalJSON(eventPayload{
		Title:         title,
		Body:          body,
		Labels:        labels,
		Assignees:     assignees,
		ChangedFields: changedFields,
		Reason:        record.StateReason,
	})
	if err != nil {
		return issuecore.Issue{}, p.operationError("update", "storage_error", err)
	}
	if err := p.appendEventTx(ctx, tx, key, "updated", defaultActor(), now, payload); err != nil {
		return issuecore.Issue{}, p.operationError("update", "storage_error", err)
	}

	issue, err := p.loadIssueByID(ctx, tx, key.IssueID, true)
	if err != nil {
		return issuecore.Issue{}, err
	}

	if err := tx.Commit(); err != nil {
		return issuecore.Issue{}, p.operationError("update", "storage_error", err)
	}
	return issue, nil
}

func (p *Provider) AddComment(ctx context.Context, locator issuecore.IssueLocator, input issuecore.AddCommentInput) (issuecore.Comment, error) {
	body := normalizeBody(input.Body)
	if strings.TrimSpace(body) == "" {
		return issuecore.Comment{}, p.operationError("comment", "invalid_argument", errors.New("comment body is required"))
	}

	db, err := p.ensureDB(ctx, "comment", p.path, p.now)
	if err != nil {
		return issuecore.Comment{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}
	defer tx.Rollback()

	key, err := p.resolveIssue(ctx, tx, "comment", locator)
	if err != nil {
		return issuecore.Comment{}, err
	}

	now := p.now().UTC()
	commentSeq, err := nextCounter(ctx, tx, counterComments)
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}
	commentID := formatCommentID(commentSeq)
	author := defaultActor()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO issue_comments (
			comment_number, comment_id, issue_id, body_markdown, body_text,
			author_login, author_type, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		commentSeq,
		commentID,
		key.IssueID,
		body,
		body,
		author.Login,
		author.Type,
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE issues
		 SET comments_count = comments_count + 1, updated_at = ?
		 WHERE issue_id = ?`,
		formatTime(now),
		key.IssueID,
	)
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}

	payload, err := marshalJSON(eventPayload{CommentID: commentID})
	if err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}
	if err := p.appendEventTx(ctx, tx, key, "commented", author, now, payload); err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}

	comment, err := p.loadCommentByID(ctx, tx, commentID)
	if err != nil {
		return issuecore.Comment{}, err
	}

	if err := tx.Commit(); err != nil {
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}
	return comment, nil
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

	db, err := p.ensureDB(ctx, "dispatch", p.path, p.now)
	if err != nil {
		return issuecore.Issue{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}
	defer tx.Rollback()

	key, err := p.resolveIssue(ctx, tx, "dispatch", locator)
	if err != nil {
		return issuecore.Issue{}, err
	}

	issue, err := p.loadIssueByID(ctx, tx, key.IssueID, false)
	if err != nil {
		return issuecore.Issue{}, err
	}

	metadata := &issuecore.DispatchMetadata{}
	if issue.Dispatch != nil {
		metadata = issue.Dispatch
	}
	metadata.Records = append(metadata.Records, record)
	latest := record
	metadata.Latest = &latest
	if err := metadata.Validate(); err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}

	dispatchJSON, err := marshalJSON(metadata)
	if err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}

	now := p.now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE issues
		 SET dispatch_json = NULLIF(?, ''), updated_at = ?
		 WHERE issue_id = ?`,
		dispatchJSON,
		formatTime(now),
		key.IssueID,
	); err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}

	issue, err = p.loadIssueByID(ctx, tx, key.IssueID, true)
	if err != nil {
		return issuecore.Issue{}, err
	}

	if err := tx.Commit(); err != nil {
		return issuecore.Issue{}, p.operationError("dispatch", "storage_error", err)
	}
	return issue, nil
}

func (p *Provider) Export(ctx context.Context) (Snapshot, error) {
	db, err := p.ensureDB(ctx, "export", p.path, p.now)
	if err != nil {
		return Snapshot{}, err
	}

	snapshot := Snapshot{
		SchemaVersion: schemaVersion(),
		Provider:      issuecore.ProviderLocal,
		Issues:        []issuecore.Issue{},
		Events:        []EventRecord{},
		ProviderRefs:  []ProviderRefRecord{},
	}

	issueIDs, err := p.listIssueIDs(ctx, db, true)
	if err != nil {
		return Snapshot{}, err
	}
	for _, issueID := range issueIDs {
		issue, err := p.loadIssueByID(ctx, db, issueID, true)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Issues = append(snapshot.Issues, issue)
	}

	snapshot.Events, err = p.loadEvents(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.ProviderRefs, err = p.loadProviderRefs(ctx, db)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (p *Provider) ExportJSON(ctx context.Context) ([]byte, error) {
	snapshot, err := p.Export(ctx)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(snapshot, "", "  ")
}

func (p *Provider) changeState(ctx context.Context, operation string, locator issuecore.IssueLocator, state issuecore.IssueState, reason issuecore.IssueStateReason) (issuecore.Issue, error) {
	db, err := p.ensureDB(ctx, operation, p.path, p.now)
	if err != nil {
		return issuecore.Issue{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}
	defer tx.Rollback()

	key, err := p.resolveIssue(ctx, tx, operation, locator)
	if err != nil {
		return issuecore.Issue{}, err
	}

	now := p.now().UTC()
	actor := defaultActor()

	var (
		closedAt    any
		closedBy    any
		closedByTyp any
	)
	if state == issuecore.IssueStateClosed {
		closedAt = formatTime(now)
		closedBy = actor.Login
		closedByTyp = actor.Type
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE issues
		 SET state = ?, state_reason = NULLIF(?, ''), updated_at = ?, closed_at = ?, closed_by_login = ?, closed_by_type = ?
		 WHERE issue_id = ?`,
		string(state),
		string(reason),
		formatTime(now),
		closedAt,
		closedBy,
		closedByTyp,
		key.IssueID,
	)
	if err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}

	kind := "closed"
	if state == issuecore.IssueStateOpen {
		kind = "reopened"
	}
	payload, err := marshalJSON(eventPayload{Reason: string(reason)})
	if err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}
	if err := p.appendEventTx(ctx, tx, key, kind, actor, now, payload); err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}

	issue, err := p.loadIssueByID(ctx, tx, key.IssueID, true)
	if err != nil {
		return issuecore.Issue{}, err
	}

	if err := tx.Commit(); err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}
	return issue, nil
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
		patch.StateReason == nil
}
