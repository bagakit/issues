package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bagakit/issues/pkg/issuecore"
)

const (
	sqliteDriver = "sqlite"

	defaultLimit = 30
	maxListLimit = 200

	counterIssues   = "issue_number"
	counterComments = "comment_number"
	counterEvents   = "event_number"
)

type dbQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type dbHandle struct {
	mu sync.Mutex
	db *sql.DB
}

type issueRecord struct {
	IssueID           string
	NodeID            string
	Repository        string
	Number            int
	URL               string
	HTMLURL           string
	Title             string
	Body              string
	BodyText          string
	State             string
	StateReason       string
	AuthorID          string
	AuthorNodeID      string
	AuthorLogin       string
	AuthorType        string
	AuthorURL         string
	AuthorHTMLURL     string
	AuthorAssociation string
	CommentsCount     int
	Locked            int
	ActiveLockReason  string
	CreatedAt         string
	UpdatedAt         string
	ClosedAt          string
	ClosedByID        string
	ClosedByNodeID    string
	ClosedByLogin     string
	ClosedByType      string
	ClosedByURL       string
	ClosedByHTMLURL   string
	DispatchJSON      string
	ProviderRawJSON   string
}

type migration struct {
	Version    int
	Statements []string
}

var migrations = []migration{
	{
		Version: 1,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS counters (
				name TEXT PRIMARY KEY,
				next_value INTEGER NOT NULL
			)`,
			`INSERT OR IGNORE INTO counters(name, next_value) VALUES
				('issue_number', 1),
				('comment_number', 1),
				('event_number', 1)`,
			`CREATE TABLE IF NOT EXISTS issues (
				issue_id TEXT PRIMARY KEY,
				provider TEXT NOT NULL,
				node_id TEXT,
				repository TEXT NOT NULL DEFAULT '',
				number INTEGER NOT NULL UNIQUE,
				url TEXT,
				html_url TEXT,
				title TEXT NOT NULL,
				body_markdown TEXT NOT NULL DEFAULT '',
				body_text TEXT NOT NULL DEFAULT '',
				state TEXT NOT NULL,
				state_reason TEXT,
				author_id TEXT,
				author_node_id TEXT,
				author_login TEXT NOT NULL,
				author_type TEXT NOT NULL DEFAULT '',
				author_url TEXT,
				author_html_url TEXT,
				author_association TEXT,
				comments_count INTEGER NOT NULL DEFAULT 0,
				locked INTEGER NOT NULL DEFAULT 0,
				active_lock_reason TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				closed_at TEXT,
				closed_by_id TEXT,
				closed_by_node_id TEXT,
				closed_by_login TEXT,
				closed_by_type TEXT,
				closed_by_url TEXT,
				closed_by_html_url TEXT,
				provider_raw_json TEXT
			)`,
			`CREATE INDEX IF NOT EXISTS issues_repository_state_number_idx
				ON issues(repository, state, number DESC)`,
			`CREATE TABLE IF NOT EXISTS issue_labels (
				issue_id TEXT NOT NULL,
				name TEXT NOT NULL,
				color TEXT,
				description TEXT,
				is_default INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY(issue_id, name),
				FOREIGN KEY(issue_id) REFERENCES issues(issue_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS issue_labels_issue_name_idx
				ON issue_labels(issue_id, name)`,
			`CREATE TABLE IF NOT EXISTS issue_assignees (
				issue_id TEXT NOT NULL,
				actor_id TEXT,
				node_id TEXT,
				login TEXT NOT NULL,
				actor_type TEXT,
				url TEXT,
				html_url TEXT,
				PRIMARY KEY(issue_id, login),
				FOREIGN KEY(issue_id) REFERENCES issues(issue_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS issue_assignees_issue_login_idx
				ON issue_assignees(issue_id, login)`,
			`CREATE TABLE IF NOT EXISTS issue_comments (
				comment_number INTEGER PRIMARY KEY,
				comment_id TEXT NOT NULL UNIQUE,
				node_id TEXT,
				issue_id TEXT NOT NULL,
				url TEXT,
				html_url TEXT,
				body_markdown TEXT NOT NULL,
				body_text TEXT NOT NULL DEFAULT '',
				author_id TEXT,
				author_node_id TEXT,
				author_login TEXT NOT NULL,
				author_type TEXT NOT NULL DEFAULT '',
				author_url TEXT,
				author_html_url TEXT,
				author_association TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				reactions_json TEXT,
				pinned INTEGER NOT NULL DEFAULT 0,
				pinned_at TEXT,
				pinned_by_id TEXT,
				pinned_by_node_id TEXT,
				pinned_by_login TEXT,
				pinned_by_type TEXT,
				pinned_by_url TEXT,
				pinned_by_html_url TEXT,
				minimized_reason TEXT,
				provider_raw_json TEXT,
				FOREIGN KEY(issue_id) REFERENCES issues(issue_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS issue_comments_issue_created_idx
				ON issue_comments(issue_id, created_at, comment_number)`,
			`CREATE TABLE IF NOT EXISTS provider_refs (
				issue_id TEXT NOT NULL,
				provider TEXT NOT NULL,
				external_id TEXT NOT NULL DEFAULT '',
				external_node_id TEXT,
				url TEXT,
				html_url TEXT,
				etag TEXT,
				metadata_json TEXT,
				PRIMARY KEY(issue_id, provider, external_id),
				FOREIGN KEY(issue_id) REFERENCES issues(issue_id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS issue_events (
				event_sequence INTEGER PRIMARY KEY,
				event_id TEXT NOT NULL UNIQUE,
				issue_id TEXT NOT NULL,
				issue_number INTEGER NOT NULL,
				kind TEXT NOT NULL,
				actor_id TEXT,
				actor_node_id TEXT,
				actor_login TEXT,
				actor_type TEXT,
				actor_url TEXT,
				actor_html_url TEXT,
				created_at TEXT NOT NULL,
				payload_json TEXT,
				provider_raw_json TEXT,
				FOREIGN KEY(issue_id) REFERENCES issues(issue_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS issue_events_issue_sequence_idx
				ON issue_events(issue_id, event_sequence)`,
		},
	},
	{
		Version: 2,
		Statements: []string{
			`ALTER TABLE issues ADD COLUMN dispatch_json TEXT`,
		},
	},
}

func (p *Provider) ensureDB(ctx context.Context, operation, path string, now func() time.Time) (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.db != nil {
		return p.db, nil
	}

	if strings.TrimSpace(path) == "" {
		return nil, p.operationError(operation, "provider_config_error", fmt.Errorf("local provider database path is required (use --db or %s)", EnvDBPath))
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, p.operationError(operation, "provider_config_error", err)
		}
	}

	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		return nil, p.operationError(operation, "provider_config_error", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, p.operationError(operation, "provider_config_error", err)
	}
	if err := applyMigrations(ctx, db, now); err != nil {
		db.Close()
		return nil, p.operationError(operation, "provider_config_error", err)
	}

	p.db = db
	return p.db, nil
}

func (h *dbHandle) closeDB() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db == nil {
		return nil
	}
	err := h.db.Close()
	h.db = nil
	return err
}

func applyMigrations(ctx context.Context, db *sql.DB, now func() time.Time) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, migration.Version).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, statement := range migration.Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				tx.Rollback()
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, migration.Version, formatTime(now().UTC())); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func schemaVersion() int {
	return len(migrations)
}

func nextCounter(ctx context.Context, tx *sql.Tx, name string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE counters SET next_value = next_value + 1 WHERE name = ?`, name); err != nil {
		return 0, err
	}

	var nextValue int64
	if err := tx.QueryRowContext(ctx, `SELECT next_value - 1 FROM counters WHERE name = ?`, name).Scan(&nextValue); err != nil {
		return 0, err
	}
	return nextValue, nil
}

func replaceLabels(ctx context.Context, tx *sql.Tx, issueID string, labels []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels WHERE issue_id = ?`, issueID); err != nil {
		return err
	}
	for _, label := range labels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issue_labels(issue_id, name) VALUES (?, ?)`, issueID, label); err != nil {
			return err
		}
	}
	return nil
}

func replaceAssignees(ctx context.Context, tx *sql.Tx, issueID string, assignees []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_assignees WHERE issue_id = ?`, issueID); err != nil {
		return err
	}
	for _, assignee := range assignees {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issue_assignees(issue_id, login, actor_type) VALUES (?, ?, ?)`, issueID, assignee, "User"); err != nil {
			return err
		}
	}
	return nil
}

func normalizeSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	if len(items) == 0 {
		return nil
	}

	sort.Strings(items)
	return items
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

func formatIssueID(number int64) string {
	return fmt.Sprintf("local-issue-%06d", number)
}

func formatCommentID(number int64) string {
	return fmt.Sprintf("local-comment-%06d", number)
}

func formatEventID(number int64) string {
	return fmt.Sprintf("local-event-%06d", number)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func parseTimePtr(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseTime(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func rawMessage(value string) json.RawMessage {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return json.RawMessage(value)
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func normalizeActorPtr(actor issuecore.Actor) *issuecore.Actor {
	if actor.ID == "" && actor.NodeID == "" && actor.Login == "" && actor.Type == "" && actor.URL == "" && actor.HTMLURL == "" {
		return nil
	}
	if actor.Type == "" {
		actor.Type = "User"
	}
	return &actor
}

func decodeReactionRollup(value string) *issuecore.ReactionRollup {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var rollup issuecore.ReactionRollup
	if err := json.Unmarshal([]byte(value), &rollup); err != nil {
		return nil
	}
	return &rollup
}
