package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bagakit/issues/pkg/issuecore"
)

func (p *Provider) listIssues(ctx context.Context, queryer dbQueryer, query issuecore.ListIssuesQuery) (issuecore.IssuePage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	state := query.State
	if state == "" {
		state = issuecore.IssueStateFilterOpen
	}

	clauses := []string{"1 = 1"}
	args := make([]any, 0, 8)

	switch state {
	case issuecore.IssueStateFilterOpen:
		clauses = append(clauses, "state = ?")
		args = append(args, string(issuecore.IssueStateOpen))
	case issuecore.IssueStateFilterClosed:
		clauses = append(clauses, "state = ?")
		args = append(args, string(issuecore.IssueStateClosed))
	case issuecore.IssueStateFilterAll:
	default:
		return issuecore.IssuePage{}, p.operationError("list", "invalid_argument", fmt.Errorf("unsupported issue state filter %q", query.State))
	}

	if repository := strings.TrimSpace(query.Repository); repository != "" {
		clauses = append(clauses, "repository = ?")
		args = append(args, repository)
	}

	if assignee := strings.TrimSpace(query.Assignee); assignee != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM issue_assignees WHERE issue_assignees.issue_id = issues.issue_id AND login = ?)")
		args = append(args, assignee)
	}

	for _, label := range normalizeSet(query.Labels) {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM issue_labels WHERE issue_labels.issue_id = issues.issue_id AND name = ?)")
		args = append(args, label)
	}

	if search := strings.TrimSpace(query.Search); search != "" {
		term := "%" + strings.ToLower(search) + "%"
		clauses = append(clauses, "(LOWER(title) LIKE ? OR LOWER(body_markdown) LIKE ?)")
		args = append(args, term, term)
	}

	if query.PageToken != "" {
		pageToken, err := strconv.Atoi(query.PageToken)
		if err != nil || pageToken <= 0 {
			return issuecore.IssuePage{}, p.operationError("list", "invalid_argument", fmt.Errorf("invalid page token %q", query.PageToken))
		}
		clauses = append(clauses, "number < ?")
		args = append(args, pageToken)
	}

	statement := fmt.Sprintf(
		`SELECT issue_id, number
		 FROM issues
		 WHERE %s
		 ORDER BY number DESC, issue_id DESC
		 LIMIT ?`,
		strings.Join(clauses, " AND "),
	)
	args = append(args, limit+1)

	rows, err := queryer.QueryContext(ctx, statement, args...)
	if err != nil {
		return issuecore.IssuePage{}, p.operationError("list", "storage_error", err)
	}
	defer rows.Close()

	keys := make([]issueKey, 0, limit+1)
	for rows.Next() {
		var key issueKey
		if err := rows.Scan(&key.IssueID, &key.Number); err != nil {
			return issuecore.IssuePage{}, p.operationError("list", "storage_error", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return issuecore.IssuePage{}, p.operationError("list", "storage_error", err)
	}

	page := issuecore.IssuePage{
		Issues: make([]issuecore.Issue, 0, limit),
	}
	if len(keys) > limit {
		page.NextPageToken = strconv.Itoa(keys[limit-1].Number)
		keys = keys[:limit]
	}

	for _, key := range keys {
		issue, err := p.loadIssueByID(ctx, queryer, "list", key.IssueID, false)
		if err != nil {
			return issuecore.IssuePage{}, err
		}
		page.Issues = append(page.Issues, issue)
	}
	return page, nil
}

func (p *Provider) listIssueIDs(ctx context.Context, queryer dbQueryer, ascending bool) ([]string, error) {
	order := "DESC"
	if ascending {
		order = "ASC"
	}

	rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`SELECT issue_id FROM issues ORDER BY number %s, issue_id %s`, order, order))
	if err != nil {
		return nil, p.operationError("export", "storage_error", err)
	}
	defer rows.Close()

	issueIDs := []string{}
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			return nil, p.operationError("export", "storage_error", err)
		}
		issueIDs = append(issueIDs, issueID)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("export", "storage_error", err)
	}
	return issueIDs, nil
}

func (p *Provider) resolveIssue(ctx context.Context, queryer dbQueryer, operation string, locator issuecore.IssueLocator) (issueKey, error) {
	if locator.Number <= 0 && strings.TrimSpace(locator.ID) == "" {
		return issueKey{}, p.operationError(operation, "invalid_argument", errors.New("issue locator requires either number or id"))
	}

	statement := `SELECT issue_id, number FROM issues WHERE `
	args := make([]any, 0, 3)
	if locator.Number > 0 {
		statement += `number = ?`
		args = append(args, locator.Number)
	} else {
		statement += `issue_id = ?`
		args = append(args, strings.TrimSpace(locator.ID))
	}
	if repository := strings.TrimSpace(locator.Repository); repository != "" {
		statement += ` AND repository = ?`
		args = append(args, repository)
	}
	statement += ` LIMIT 1`

	var key issueKey
	if err := queryer.QueryRowContext(ctx, statement, args...).Scan(&key.IssueID, &key.Number); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return issueKey{}, p.operationError(operation, "not_found", err)
		}
		return issueKey{}, p.operationError(operation, "storage_error", err)
	}
	return key, nil
}

func (p *Provider) loadIssueByID(ctx context.Context, queryer dbQueryer, operation, issueID string, includeDetails bool) (issuecore.Issue, error) {
	record, err := p.loadIssueRecord(ctx, queryer, issueID)
	if err != nil {
		return issuecore.Issue{}, p.withOperation(err, operation)
	}

	labels, err := p.loadLabels(ctx, queryer, issueID)
	if err != nil {
		return issuecore.Issue{}, p.withOperation(err, operation)
	}
	assignees, err := p.loadAssignees(ctx, queryer, issueID)
	if err != nil {
		return issuecore.Issue{}, p.withOperation(err, operation)
	}

	var comments []issuecore.Comment
	var timeline []issuecore.TimelineEvent
	if includeDetails {
		comments, err = p.loadComments(ctx, queryer, issueID)
		if err != nil {
			return issuecore.Issue{}, p.withOperation(err, operation)
		}
		timeline, err = p.loadTimeline(ctx, queryer, issueID)
		if err != nil {
			return issuecore.Issue{}, p.withOperation(err, operation)
		}
	}

	issue, err := buildIssue(record, labels, assignees, comments, timeline)
	if err != nil {
		return issuecore.Issue{}, p.operationError(operation, "storage_error", err)
	}
	return issue, nil
}

func (p *Provider) withOperation(err error, operation string) error {
	if err == nil {
		return nil
	}

	var opErr *issuecore.OperationError
	if !errors.As(err, &opErr) || opErr.Provider != issuecore.ProviderLocal || opErr.Operation == operation {
		return err
	}

	return &issuecore.OperationError{
		Code:      opErr.Code,
		Provider:  opErr.Provider,
		Operation: operation,
		Err:       opErr.Err,
	}
}

func (p *Provider) loadIssueRecord(ctx context.Context, queryer dbQueryer, issueID string) (issueRecord, error) {
	statement := `SELECT
		issue_id,
		COALESCE(node_id, ''),
		repository,
		number,
		COALESCE(url, ''),
		COALESCE(html_url, ''),
		title,
		body_markdown,
		body_text,
		state,
		COALESCE(state_reason, ''),
		COALESCE(author_id, ''),
		COALESCE(author_node_id, ''),
		author_login,
		author_type,
		COALESCE(author_url, ''),
		COALESCE(author_html_url, ''),
		COALESCE(author_association, ''),
		comments_count,
		locked,
		COALESCE(active_lock_reason, ''),
		created_at,
		updated_at,
		COALESCE(closed_at, ''),
		COALESCE(closed_by_id, ''),
		COALESCE(closed_by_node_id, ''),
		COALESCE(closed_by_login, ''),
		COALESCE(closed_by_type, ''),
		COALESCE(closed_by_url, ''),
		COALESCE(closed_by_html_url, ''),
		COALESCE(dispatch_json, ''),
		COALESCE(provider_raw_json, '')
	FROM issues
	WHERE issue_id = ?`

	var record issueRecord
	if err := queryer.QueryRowContext(ctx, statement, issueID).Scan(
		&record.IssueID,
		&record.NodeID,
		&record.Repository,
		&record.Number,
		&record.URL,
		&record.HTMLURL,
		&record.Title,
		&record.Body,
		&record.BodyText,
		&record.State,
		&record.StateReason,
		&record.AuthorID,
		&record.AuthorNodeID,
		&record.AuthorLogin,
		&record.AuthorType,
		&record.AuthorURL,
		&record.AuthorHTMLURL,
		&record.AuthorAssociation,
		&record.CommentsCount,
		&record.Locked,
		&record.ActiveLockReason,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.ClosedAt,
		&record.ClosedByID,
		&record.ClosedByNodeID,
		&record.ClosedByLogin,
		&record.ClosedByType,
		&record.ClosedByURL,
		&record.ClosedByHTMLURL,
		&record.DispatchJSON,
		&record.ProviderRawJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return issueRecord{}, p.operationError("get", "not_found", err)
		}
		return issueRecord{}, p.operationError("get", "storage_error", err)
	}
	return record, nil
}

func (p *Provider) loadLabels(ctx context.Context, queryer dbQueryer, issueID string) ([]issuecore.Label, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT name, COALESCE(color, ''), COALESCE(description, ''), is_default FROM issue_labels WHERE issue_id = ? ORDER BY name ASC`, issueID)
	if err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	defer rows.Close()

	labels := []issuecore.Label{}
	for rows.Next() {
		var (
			label     issuecore.Label
			isDefault int
		)
		if err := rows.Scan(&label.Name, &label.Color, &label.Description, &isDefault); err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		label.IsDefault = isDefault == 1
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	return labels, nil
}

func (p *Provider) loadAssignees(ctx context.Context, queryer dbQueryer, issueID string) ([]issuecore.Actor, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT COALESCE(actor_id, ''), COALESCE(node_id, ''), login, COALESCE(actor_type, ''), COALESCE(url, ''), COALESCE(html_url, '') FROM issue_assignees WHERE issue_id = ? ORDER BY login ASC`, issueID)
	if err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	defer rows.Close()

	assignees := []issuecore.Actor{}
	for rows.Next() {
		var actor issuecore.Actor
		if err := rows.Scan(&actor.ID, &actor.NodeID, &actor.Login, &actor.Type, &actor.URL, &actor.HTMLURL); err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		if actor.Type == "" {
			actor.Type = "User"
		}
		assignees = append(assignees, actor)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	return assignees, nil
}

func (p *Provider) loadComments(ctx context.Context, queryer dbQueryer, issueID string) ([]issuecore.Comment, error) {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT
			comment_id,
			COALESCE(node_id, ''),
			COALESCE(url, ''),
			COALESCE(html_url, ''),
			body_markdown,
			body_text,
			COALESCE(author_id, ''),
			COALESCE(author_node_id, ''),
			author_login,
			author_type,
			COALESCE(author_url, ''),
			COALESCE(author_html_url, ''),
			COALESCE(author_association, ''),
			created_at,
			updated_at,
			COALESCE(reactions_json, ''),
			pinned,
			COALESCE(pinned_at, ''),
			COALESCE(pinned_by_id, ''),
			COALESCE(pinned_by_node_id, ''),
			COALESCE(pinned_by_login, ''),
			COALESCE(pinned_by_type, ''),
			COALESCE(pinned_by_url, ''),
			COALESCE(pinned_by_html_url, ''),
			COALESCE(minimized_reason, ''),
			COALESCE(provider_raw_json, '')
		FROM issue_comments
		WHERE issue_id = ?
		ORDER BY created_at ASC, comment_number ASC`,
		issueID,
	)
	if err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	defer rows.Close()

	comments := []issuecore.Comment{}
	for rows.Next() {
		var (
			comment         issuecore.Comment
			author          issuecore.Actor
			createdAt       string
			updatedAt       string
			reactionsJSON   string
			pinned          int
			pinnedAt        string
			pinnedBy        issuecore.Actor
			minimizedReason string
			providerRawJSON string
		)
		if err := rows.Scan(
			&comment.ID,
			&comment.NodeID,
			&comment.URL,
			&comment.HTMLURL,
			&comment.Body,
			&comment.BodyText,
			&author.ID,
			&author.NodeID,
			&author.Login,
			&author.Type,
			&author.URL,
			&author.HTMLURL,
			&comment.AuthorAssociation,
			&createdAt,
			&updatedAt,
			&reactionsJSON,
			&pinned,
			&pinnedAt,
			&pinnedBy.ID,
			&pinnedBy.NodeID,
			&pinnedBy.Login,
			&pinnedBy.Type,
			&pinnedBy.URL,
			&pinnedBy.HTMLURL,
			&minimizedReason,
			&providerRawJSON,
		); err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		var err error
		comment.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		comment.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		comment.User = normalizeActorPtr(author)
		comment.Pinned = pinned == 1
		comment.PinnedAt, err = parseTimePtr(pinnedAt)
		if err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		comment.PinnedBy = normalizeActorPtr(pinnedBy)
		comment.MinimizedReason = minimizedReason
		comment.Reactions = decodeReactionRollup(reactionsJSON)
		comment.ProviderRaw = rawMessage(providerRawJSON)
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	return comments, nil
}

func (p *Provider) loadCommentByID(ctx context.Context, queryer dbQueryer, commentID string) (issuecore.Comment, error) {
	row := queryer.QueryRowContext(ctx, `SELECT issue_id FROM issue_comments WHERE comment_id = ? LIMIT 1`, commentID)
	var issueID string
	if err := row.Scan(&issueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return issuecore.Comment{}, p.operationError("comment", "not_found", err)
		}
		return issuecore.Comment{}, p.operationError("comment", "storage_error", err)
	}

	comments, err := p.loadComments(ctx, queryer, issueID)
	if err != nil {
		return issuecore.Comment{}, err
	}
	for _, comment := range comments {
		if comment.ID == commentID {
			return comment, nil
		}
	}
	return issuecore.Comment{}, p.operationError("comment", "not_found", sql.ErrNoRows)
}

func (p *Provider) loadTimeline(ctx context.Context, queryer dbQueryer, issueID string) ([]issuecore.TimelineEvent, error) {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT
			event_id,
			kind,
			COALESCE(actor_id, ''),
			COALESCE(actor_node_id, ''),
			COALESCE(actor_login, ''),
			COALESCE(actor_type, ''),
			COALESCE(actor_url, ''),
			COALESCE(actor_html_url, ''),
			created_at,
			COALESCE(payload_json, ''),
			COALESCE(provider_raw_json, '')
		FROM issue_events
		WHERE issue_id = ? AND kind <> 'commented'
		ORDER BY event_sequence ASC`,
		issueID,
	)
	if err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	defer rows.Close()

	timeline := []issuecore.TimelineEvent{}
	for rows.Next() {
		var (
			event           issuecore.TimelineEvent
			actor           issuecore.Actor
			createdAt       string
			payloadJSON     string
			providerRawJSON string
		)
		if err := rows.Scan(
			&event.ID,
			&event.Kind,
			&actor.ID,
			&actor.NodeID,
			&actor.Login,
			&actor.Type,
			&actor.URL,
			&actor.HTMLURL,
			&createdAt,
			&payloadJSON,
			&providerRawJSON,
		); err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		var err error
		event.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, p.operationError("get", "storage_error", err)
		}
		event.Actor = normalizeActorPtr(actor)
		event.Payload = rawMessage(payloadJSON)
		event.ProviderRaw = rawMessage(providerRawJSON)
		timeline = append(timeline, event)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("get", "storage_error", err)
	}
	return timeline, nil
}

func (p *Provider) loadEvents(ctx context.Context, queryer dbQueryer) ([]EventRecord, error) {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT
			event_sequence,
			event_id,
			issue_id,
			issue_number,
			kind,
			COALESCE(actor_id, ''),
			COALESCE(actor_node_id, ''),
			COALESCE(actor_login, ''),
			COALESCE(actor_type, ''),
			COALESCE(actor_url, ''),
			COALESCE(actor_html_url, ''),
			created_at,
			COALESCE(payload_json, ''),
			COALESCE(provider_raw_json, '')
		FROM issue_events
		ORDER BY event_sequence ASC`,
	)
	if err != nil {
		return nil, p.operationError("export", "storage_error", err)
	}
	defer rows.Close()

	events := []EventRecord{}
	for rows.Next() {
		var (
			event           EventRecord
			actor           issuecore.Actor
			createdAt       string
			payloadJSON     string
			providerRawJSON string
		)
		if err := rows.Scan(
			&event.Sequence,
			&event.EventID,
			&event.IssueID,
			&event.IssueNumber,
			&event.Kind,
			&actor.ID,
			&actor.NodeID,
			&actor.Login,
			&actor.Type,
			&actor.URL,
			&actor.HTMLURL,
			&createdAt,
			&payloadJSON,
			&providerRawJSON,
		); err != nil {
			return nil, p.operationError("export", "storage_error", err)
		}
		var err error
		event.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, p.operationError("export", "storage_error", err)
		}
		event.Actor = normalizeActorPtr(actor)
		event.Payload = rawMessage(payloadJSON)
		event.ProviderRaw = rawMessage(providerRawJSON)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("export", "storage_error", err)
	}
	return events, nil
}

func (p *Provider) loadProviderRefs(ctx context.Context, queryer dbQueryer) ([]ProviderRefRecord, error) {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT
			issue_id,
			provider,
			external_id,
			COALESCE(external_node_id, ''),
			COALESCE(url, ''),
			COALESCE(html_url, ''),
			COALESCE(etag, ''),
			COALESCE(metadata_json, '')
		FROM provider_refs
		ORDER BY issue_id ASC, provider ASC, external_id ASC`,
	)
	if err != nil {
		return nil, p.operationError("export", "storage_error", err)
	}
	defer rows.Close()

	refs := []ProviderRefRecord{}
	for rows.Next() {
		var (
			ref          ProviderRefRecord
			metadataJSON string
		)
		if err := rows.Scan(
			&ref.IssueID,
			&ref.Provider,
			&ref.ExternalID,
			&ref.ExternalNodeID,
			&ref.URL,
			&ref.HTMLURL,
			&ref.ETag,
			&metadataJSON,
		); err != nil {
			return nil, p.operationError("export", "storage_error", err)
		}
		ref.Metadata = rawMessage(metadataJSON)
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, p.operationError("export", "storage_error", err)
	}
	return refs, nil
}

func (p *Provider) appendEventTx(ctx context.Context, tx *sql.Tx, key issueKey, kind string, actor *issuecore.Actor, createdAt time.Time, payload string) error {
	sequence, err := nextCounter(ctx, tx, counterEvents)
	if err != nil {
		return err
	}

	var actorValue issuecore.Actor
	if actor != nil {
		actorValue = *actor
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO issue_events (
			event_sequence, event_id, issue_id, issue_number, kind,
			actor_id, actor_node_id, actor_login, actor_type, actor_url, actor_html_url,
			created_at, payload_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		sequence,
		formatEventID(sequence),
		key.IssueID,
		key.Number,
		kind,
		nullIfEmpty(actorValue.ID),
		nullIfEmpty(actorValue.NodeID),
		nullIfEmpty(actorValue.Login),
		nullIfEmpty(actorValue.Type),
		nullIfEmpty(actorValue.URL),
		nullIfEmpty(actorValue.HTMLURL),
		formatTime(createdAt),
		payload,
	)
	return err
}

func buildIssue(record issueRecord, labels []issuecore.Label, assignees []issuecore.Actor, comments []issuecore.Comment, timeline []issuecore.TimelineEvent) (issuecore.Issue, error) {
	createdAt, err := parseTime(record.CreatedAt)
	if err != nil {
		return issuecore.Issue{}, err
	}
	updatedAt, err := parseTime(record.UpdatedAt)
	if err != nil {
		return issuecore.Issue{}, err
	}

	issue := issuecore.Issue{
		Provider:          issuecore.ProviderLocal,
		Repository:        record.Repository,
		ID:                record.IssueID,
		NodeID:            record.NodeID,
		Number:            record.Number,
		URL:               record.URL,
		HTMLURL:           record.HTMLURL,
		Title:             record.Title,
		Body:              record.Body,
		BodyText:          record.BodyText,
		State:             issuecore.IssueState(record.State),
		StateReason:       issuecore.IssueStateReason(record.StateReason),
		User:              normalizeActorPtr(issuecore.Actor{ID: record.AuthorID, NodeID: record.AuthorNodeID, Login: record.AuthorLogin, Type: record.AuthorType, URL: record.AuthorURL, HTMLURL: record.AuthorHTMLURL}),
		AuthorAssociation: record.AuthorAssociation,
		Labels:            labels,
		Assignees:         assignees,
		Comments:          record.CommentsCount,
		CommentItems:      comments,
		Locked:            record.Locked == 1,
		ActiveLockReason:  record.ActiveLockReason,
		Timeline:          timeline,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ProviderRaw:       rawMessage(record.ProviderRawJSON),
	}

	if len(assignees) > 0 {
		assignee := assignees[0]
		issue.Assignee = &assignee
	}

	issue.ClosedAt, err = parseTimePtr(record.ClosedAt)
	if err != nil {
		return issuecore.Issue{}, err
	}
	issue.ClosedBy = normalizeActorPtr(issuecore.Actor{
		ID:      record.ClosedByID,
		NodeID:  record.ClosedByNodeID,
		Login:   record.ClosedByLogin,
		Type:    record.ClosedByType,
		URL:     record.ClosedByURL,
		HTMLURL: record.ClosedByHTMLURL,
	})
	issue.Dispatch, err = decodeDispatchMetadata(record.DispatchJSON)
	if err != nil {
		return issuecore.Issue{}, err
	}

	return issue, nil
}

func decodeDispatchMetadata(value string) (*issuecore.DispatchMetadata, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var metadata issuecore.DispatchMetadata
	if err := json.Unmarshal([]byte(value), &metadata); err != nil {
		return nil, err
	}
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	return &metadata, nil
}
