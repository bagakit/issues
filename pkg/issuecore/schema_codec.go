package issuecore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	schemaMarkdownMediaType = "text/markdown"
	schemaJSONMediaType     = "application/json"

	IssueDocumentSchemaVersion      = "issues.issue.v1"
	CommentDocumentSchemaVersion    = "issues.comment.v1"
	TimelineEventSchemaVersion      = "issues.timeline_event.v1"
	ProviderIdentitySchemaVersion   = "issues.provider_identity.v1"
	PullRequestLinksSchemaVersion   = "issues.pull_requests.v1"
	DispatchRecordFileSchemaVersion = "issues.dispatch_record.v1"
	ExtensionRecordSchemaVersion    = "issues.extension.v1"
)

type IssueDocument struct {
	SchemaVersion     string           `yaml:"schema_version" json:"schema_version"`
	ID                string           `yaml:"id" json:"id"`
	PrimaryProvider   string           `yaml:"primary_provider,omitempty" json:"primary_provider,omitempty"`
	Title             string           `yaml:"title" json:"title"`
	State             IssueState       `yaml:"state" json:"state"`
	StateReason       IssueStateReason `yaml:"state_reason,omitempty" json:"state_reason,omitempty"`
	User              *Actor           `yaml:"user,omitempty" json:"user,omitempty"`
	AuthorAssociation string           `yaml:"author_association,omitempty" json:"author_association,omitempty"`
	Labels            []Label          `yaml:"labels,omitempty" json:"labels,omitempty"`
	Milestone         *Milestone       `yaml:"milestone,omitempty" json:"milestone,omitempty"`
	Assignee          *Actor           `yaml:"assignee,omitempty" json:"assignee,omitempty"`
	Assignees         []Actor          `yaml:"assignees,omitempty" json:"assignees,omitempty"`
	Comments          int              `yaml:"comments,omitempty" json:"comments,omitempty"`
	Locked            bool             `yaml:"locked,omitempty" json:"locked,omitempty"`
	ActiveLockReason  string           `yaml:"active_lock_reason,omitempty" json:"active_lock_reason,omitempty"`
	Reactions         *ReactionRollup  `yaml:"reactions,omitempty" json:"reactions,omitempty"`
	CreatedAt         time.Time        `yaml:"created_at" json:"created_at"`
	UpdatedAt         time.Time        `yaml:"updated_at" json:"updated_at"`
	ClosedAt          *time.Time       `yaml:"closed_at,omitempty" json:"closed_at,omitempty"`
	ClosedBy          *Actor           `yaml:"closed_by,omitempty" json:"closed_by,omitempty"`
	BodyText          string           `yaml:"body_text,omitempty" json:"body_text,omitempty"`
	Body              string           `yaml:"-" json:"body,omitempty"`
}

type CommentDocument struct {
	SchemaVersion     string          `yaml:"schema_version" json:"schema_version"`
	IssueID           string          `yaml:"issue_id" json:"issue_id"`
	Ordinal           int             `yaml:"ordinal" json:"ordinal"`
	ID                string          `yaml:"id,omitempty" json:"id,omitempty"`
	NodeID            string          `yaml:"node_id,omitempty" json:"node_id,omitempty"`
	URL               string          `yaml:"url,omitempty" json:"url,omitempty"`
	HTMLURL           string          `yaml:"html_url,omitempty" json:"html_url,omitempty"`
	User              *Actor          `yaml:"user,omitempty" json:"user,omitempty"`
	AuthorAssociation string          `yaml:"author_association,omitempty" json:"author_association,omitempty"`
	CreatedAt         time.Time       `yaml:"created_at" json:"created_at"`
	UpdatedAt         time.Time       `yaml:"updated_at" json:"updated_at"`
	Reactions         *ReactionRollup `yaml:"reactions,omitempty" json:"reactions,omitempty"`
	Pinned            bool            `yaml:"pinned,omitempty" json:"pinned,omitempty"`
	PinnedAt          *time.Time      `yaml:"pinned_at,omitempty" json:"pinned_at,omitempty"`
	PinnedBy          *Actor          `yaml:"pinned_by,omitempty" json:"pinned_by,omitempty"`
	MinimizedReason   string          `yaml:"minimized_reason,omitempty" json:"minimized_reason,omitempty"`
	ProviderRaw       json.RawMessage `yaml:"-" json:"provider_raw,omitempty"`
	BodyText          string          `yaml:"body_text,omitempty" json:"body_text,omitempty"`
	Body              string          `yaml:"-" json:"body,omitempty"`
}

type commentDocumentFrontmatter struct {
	CommentDocument `yaml:",inline"`
	ProviderRaw     string `yaml:"provider_raw,omitempty"`
}

type TimelineEventRecord struct {
	SchemaVersion string          `json:"schema_version"`
	IssueID       string          `json:"issue_id"`
	Ordinal       int             `json:"ordinal"`
	ID            string          `json:"id,omitempty"`
	NodeID        string          `json:"node_id,omitempty"`
	Kind          string          `json:"kind"`
	Actor         *Actor          `json:"actor,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	ProviderRaw   json.RawMessage `json:"provider_raw,omitempty"`
}

type ProviderIdentityRecord struct {
	SchemaVersion string          `json:"schema_version"`
	IssueID       string          `json:"issue_id"`
	Provider      string          `json:"provider"`
	ExternalID    string          `json:"external_id,omitempty"`
	NodeID        string          `json:"node_id,omitempty"`
	Repository    string          `json:"repository,omitempty"`
	Number        int             `json:"number,omitempty"`
	URL           string          `json:"url,omitempty"`
	HTMLURL       string          `json:"html_url,omitempty"`
	ProviderRaw   json.RawMessage `json:"provider_raw,omitempty"`
}

type PullRequestLinksRecord struct {
	SchemaVersion      string           `json:"schema_version"`
	IssueID            string           `json:"issue_id"`
	PullRequest        *PullRequestRef  `json:"pull_request,omitempty"`
	LinkedPullRequests []PullRequestRef `json:"linked_pull_requests,omitempty"`
}

type DispatchRecordFile struct {
	SchemaVersion string         `json:"schema_version"`
	IssueID       string         `json:"issue_id"`
	Ordinal       int            `json:"ordinal"`
	Record        DispatchRecord `json:"record"`
}

type ExtensionRecord struct {
	SchemaVersion string          `json:"schema_version"`
	IssueID       string          `json:"issue_id"`
	Namespace     string          `json:"namespace"`
	Data          json.RawMessage `json:"data"`
}

func ValidateIssueDocument(document IssueDocument) error {
	return document.Validate()
}

func ValidateCommentDocument(document CommentDocument) error {
	return document.Validate()
}

func ValidateTimelineEventRecord(record TimelineEventRecord) error {
	return record.Validate()
}

func ValidateProviderIdentityRecord(record ProviderIdentityRecord) error {
	return record.Validate()
}

func ValidatePullRequestLinksRecord(record PullRequestLinksRecord) error {
	return record.Validate()
}

func ValidateDispatchRecordFile(record DispatchRecordFile) error {
	return record.Validate()
}

func ValidateExtensionRecord(record ExtensionRecord) error {
	return record.Validate()
}

func (document IssueDocument) Validate() error {
	if document.SchemaVersion != IssueDocumentSchemaVersion {
		return fmt.Errorf("issue document schema version must be %q", IssueDocumentSchemaVersion)
	}
	if _, err := ParseIssueID(document.ID); err != nil {
		return fmt.Errorf("issue document id: %w", err)
	}
	if document.PrimaryProvider != "" {
		if err := ValidateProviderToken(document.PrimaryProvider); err != nil {
			return fmt.Errorf("issue document primary_provider: %w", err)
		}
	}
	if strings.TrimSpace(document.Title) == "" {
		return fmt.Errorf("issue document title is required")
	}
	if err := validateIssueState(document.State); err != nil {
		return fmt.Errorf("issue document state: %w", err)
	}
	if err := validateIssueStateReason(document.StateReason); err != nil {
		return fmt.Errorf("issue document state_reason: %w", err)
	}
	if document.Comments < 0 {
		return fmt.Errorf("issue document comments must be non-negative")
	}
	if document.CreatedAt.IsZero() {
		return fmt.Errorf("issue document created_at is required")
	}
	if document.UpdatedAt.IsZero() {
		return fmt.Errorf("issue document updated_at is required")
	}
	return nil
}

func (document CommentDocument) Validate() error {
	if document.SchemaVersion != CommentDocumentSchemaVersion {
		return fmt.Errorf("comment document schema version must be %q", CommentDocumentSchemaVersion)
	}
	if _, err := ParseIssueID(document.IssueID); err != nil {
		return fmt.Errorf("comment document issue_id: %w", err)
	}
	if document.Ordinal <= 0 {
		return fmt.Errorf("comment document ordinal must be positive")
	}
	if strings.TrimSpace(document.ID) != document.ID {
		return fmt.Errorf("comment document id must not contain leading or trailing whitespace")
	}
	if document.CreatedAt.IsZero() {
		return fmt.Errorf("comment document created_at is required")
	}
	if document.UpdatedAt.IsZero() {
		return fmt.Errorf("comment document updated_at is required")
	}
	if err := validateJSONRaw("comment document provider_raw", document.ProviderRaw); err != nil {
		return err
	}
	return nil
}

func (record TimelineEventRecord) Validate() error {
	if record.SchemaVersion != TimelineEventSchemaVersion {
		return fmt.Errorf("timeline event schema version must be %q", TimelineEventSchemaVersion)
	}
	if _, err := ParseIssueID(record.IssueID); err != nil {
		return fmt.Errorf("timeline event issue_id: %w", err)
	}
	if record.Ordinal <= 0 {
		return fmt.Errorf("timeline event ordinal must be positive")
	}
	if strings.TrimSpace(record.ID) != record.ID {
		return fmt.Errorf("timeline event id must not contain leading or trailing whitespace")
	}
	if strings.TrimSpace(record.Kind) == "" {
		return fmt.Errorf("timeline event kind is required")
	}
	if record.CreatedAt.IsZero() {
		return fmt.Errorf("timeline event created_at is required")
	}
	if err := validateJSONRaw("timeline event payload", record.Payload); err != nil {
		return err
	}
	if err := validateJSONRaw("timeline event provider_raw", record.ProviderRaw); err != nil {
		return err
	}
	return nil
}

func (record ProviderIdentityRecord) Validate() error {
	if record.SchemaVersion != ProviderIdentitySchemaVersion {
		return fmt.Errorf("provider identity schema version must be %q", ProviderIdentitySchemaVersion)
	}
	if _, err := ParseIssueID(record.IssueID); err != nil {
		return fmt.Errorf("provider identity issue_id: %w", err)
	}
	if err := ValidateProviderToken(record.Provider); err != nil {
		return fmt.Errorf("provider identity provider: %w", err)
	}
	if record.Number < 0 {
		return fmt.Errorf("provider identity number must be non-negative")
	}
	if err := validateJSONRaw("provider identity provider_raw", record.ProviderRaw); err != nil {
		return err
	}
	return nil
}

func (record PullRequestLinksRecord) Validate() error {
	if record.SchemaVersion != PullRequestLinksSchemaVersion {
		return fmt.Errorf("pull request links schema version must be %q", PullRequestLinksSchemaVersion)
	}
	if _, err := ParseIssueID(record.IssueID); err != nil {
		return fmt.Errorf("pull request links issue_id: %w", err)
	}
	if err := validatePullRequestRef(record.PullRequest, "pull_request"); err != nil {
		return err
	}
	for idx := range record.LinkedPullRequests {
		if err := validatePullRequestRef(&record.LinkedPullRequests[idx], fmt.Sprintf("linked_pull_requests[%d]", idx)); err != nil {
			return err
		}
	}
	return nil
}

func (record DispatchRecordFile) Validate() error {
	if record.SchemaVersion != DispatchRecordFileSchemaVersion {
		return fmt.Errorf("dispatch record schema version must be %q", DispatchRecordFileSchemaVersion)
	}
	if _, err := ParseIssueID(record.IssueID); err != nil {
		return fmt.Errorf("dispatch record issue_id: %w", err)
	}
	if record.Ordinal <= 0 {
		return fmt.Errorf("dispatch record ordinal must be positive")
	}
	if err := record.Record.Validate(); err != nil {
		return fmt.Errorf("dispatch record: %w", err)
	}
	return nil
}

func (record ExtensionRecord) Validate() error {
	if record.SchemaVersion != ExtensionRecordSchemaVersion {
		return fmt.Errorf("extension schema version must be %q", ExtensionRecordSchemaVersion)
	}
	if _, err := ParseIssueID(record.IssueID); err != nil {
		return fmt.Errorf("extension issue_id: %w", err)
	}
	if err := ValidateExtensionNamespace(record.Namespace); err != nil {
		return fmt.Errorf("extension namespace: %w", err)
	}
	if err := validateJSONObject("extension data", record.Data); err != nil {
		return err
	}
	return nil
}

func (document IssueDocument) MarshalMarkdown() ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	return marshalMarkdownDocument(document, document.Body)
}

func (document CommentDocument) MarshalMarkdown() ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	frontmatter := commentDocumentFrontmatter{
		CommentDocument: document,
	}
	if len(document.ProviderRaw) > 0 {
		frontmatter.ProviderRaw = string(document.ProviderRaw)
	}
	return marshalMarkdownDocument(frontmatter, document.Body)
}

func ParseIssueDocumentMarkdown(content []byte) (IssueDocument, error) {
	var document IssueDocument
	body, err := parseMarkdownDocument(content, &document)
	if err != nil {
		return IssueDocument{}, err
	}
	document.Body = body
	if err := document.Validate(); err != nil {
		return IssueDocument{}, err
	}
	return document, nil
}

func ParseCommentDocumentMarkdown(content []byte) (CommentDocument, error) {
	var frontmatter commentDocumentFrontmatter
	body, err := parseMarkdownDocument(content, &frontmatter)
	if err != nil {
		return CommentDocument{}, err
	}
	document := frontmatter.CommentDocument
	document.Body = body
	if strings.TrimSpace(frontmatter.ProviderRaw) != "" {
		document.ProviderRaw = json.RawMessage(frontmatter.ProviderRaw)
	}
	if err := document.Validate(); err != nil {
		return CommentDocument{}, err
	}
	return document, nil
}

func marshalSchemaJSON(value interface{}) ([]byte, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal schema json: %w", err)
	}
	return content, nil
}

func parseSchemaJSON(content []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode schema json: %w", err)
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return fmt.Errorf("decode schema json: %w", err)
	}
	return nil
}

func marshalMarkdownDocument(frontmatter interface{}, body string) ([]byte, error) {
	header, err := yaml.Marshal(frontmatter)
	if err != nil {
		return nil, fmt.Errorf("marshal markdown frontmatter: %w", err)
	}
	header = bytes.TrimSpace(header)

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.Write(header)
	builder.WriteString("\n---\n")
	builder.WriteString(body)
	return []byte(builder.String()), nil
}

func parseMarkdownDocument(content []byte, target interface{}) (string, error) {
	header, body, err := splitMarkdownFrontmatter(content)
	if err != nil {
		return "", err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(header))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return "", fmt.Errorf("decode markdown frontmatter: %w", err)
	}
	if err := ensureYAMLDecoderEOF(decoder); err != nil {
		return "", fmt.Errorf("decode markdown frontmatter: %w", err)
	}

	return body, nil
}

func splitMarkdownFrontmatter(content []byte) ([]byte, string, error) {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return nil, "", fmt.Errorf("markdown frontmatter must start with %q", "---")
	}

	rest := content[len("---\n"):]
	if idx := bytes.Index(rest, []byte("\n---\n")); idx >= 0 {
		return append([]byte(nil), rest[:idx+1]...), string(rest[idx+len("\n---\n"):]), nil
	}
	if bytes.HasSuffix(rest, []byte("\n---")) {
		return append([]byte(nil), rest[:len(rest)-len("\n---")+1]...), "", nil
	}

	return nil, "", fmt.Errorf("markdown frontmatter closing fence is required")
}

func ensureDecoderEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("expected a single json document")
		}
		return err
	}
	return nil
}

func ensureYAMLDecoderEOF(decoder *yaml.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("expected a single yaml document")
		}
		return err
	}
	return nil
}

func validateIssueState(state IssueState) error {
	switch state {
	case IssueStateOpen, IssueStateClosed:
		return nil
	default:
		return fmt.Errorf("unsupported issue state %q", state)
	}
}

func validateIssueStateReason(reason IssueStateReason) error {
	switch reason {
	case "":
		return nil
	case IssueStateReasonCompleted, IssueStateReasonDuplicate, IssueStateReasonNotPlanned, IssueStateReasonReopened:
		return nil
	default:
		return fmt.Errorf("unsupported issue state reason %q", reason)
	}
}

func validatePullRequestState(state PullRequestState) error {
	switch state {
	case "", PullRequestStateOpen, PullRequestStateClosed, PullRequestStateMerged:
		return nil
	default:
		return fmt.Errorf("unsupported pull request state %q", state)
	}
}

func validatePullRequestRef(pr *PullRequestRef, label string) error {
	if pr == nil {
		return nil
	}
	if pr.Number < 0 {
		return fmt.Errorf("%s number must be non-negative", label)
	}
	if err := validatePullRequestState(pr.State); err != nil {
		return fmt.Errorf("%s state: %w", label, err)
	}
	if err := validateJSONRaw(label+" provider_raw", pr.ProviderRaw); err != nil {
		return err
	}
	return nil
}

func validateJSONRaw(label string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%s must be valid JSON", label)
	}
	return nil
}

func validateJSONObject(label string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("%s is required", label)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("%s must be a JSON object", label)
	}
	if !json.Valid(trimmed) {
		return fmt.Errorf("%s must be valid JSON", label)
	}

	var object map[string]json.RawMessage
	if err := parseSchemaJSON(trimmed, &object); err != nil {
		return fmt.Errorf("%s must be a JSON object: %w", label, err)
	}
	return nil
}
