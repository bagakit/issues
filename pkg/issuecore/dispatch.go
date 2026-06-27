package issuecore

import (
	"fmt"
	"strings"
	"time"
)

type ContextFormat string

const (
	ContextFormatJSON   ContextFormat = "json"
	ContextFormatPrompt ContextFormat = "prompt"
)

type DispatchTerminalMode string

const (
	DispatchTerminalModeReuseExisting DispatchTerminalMode = "reuse_existing"
	DispatchTerminalModeCreateNew     DispatchTerminalMode = "create_new"
)

type DispatchOutcome string

const (
	DispatchOutcomePending   DispatchOutcome = "pending"
	DispatchOutcomeDelivered DispatchOutcome = "delivered"
	DispatchOutcomeFailed    DispatchOutcome = "failed"
	DispatchOutcomeCancelled DispatchOutcome = "cancelled"
)

type DispatchTargetGroup struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type ExistingTerminal struct {
	ID               string `json:"id"`
	Title            string `json:"title,omitempty"`
	RuntimePreserved bool   `json:"runtime_preserved"`
	RuntimeIdentity  string `json:"runtime_identity,omitempty"`
}

type RuntimeSelection struct {
	Agent    string            `json:"agent,omitempty"`
	Runtime  string            `json:"runtime,omitempty"`
	Profile  string            `json:"profile,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type NewTerminal struct {
	Title   string            `json:"title,omitempty"`
	Runtime *RuntimeSelection `json:"runtime,omitempty"`
}

type DispatchTerminal struct {
	Mode     DispatchTerminalMode `json:"mode"`
	Existing *ExistingTerminal    `json:"existing_terminal,omitempty"`
	New      *NewTerminal         `json:"new_terminal,omitempty"`
}

type IssueContextLink struct {
	SchemaVersion string        `json:"schema_version"`
	Format        ContextFormat `json:"format,omitempty"`
	Provider      string        `json:"provider"`
	Repository    string        `json:"repository,omitempty"`
	IssueID       string        `json:"issue_id,omitempty"`
	IssueNumber   int           `json:"issue_number,omitempty"`
	HTMLURL       string        `json:"html_url,omitempty"`
}

type DispatchRecord struct {
	ID           string              `json:"id,omitempty"`
	TargetGroup  DispatchTargetGroup `json:"target_group"`
	Terminal     DispatchTerminal    `json:"terminal"`
	DispatchedAt time.Time           `json:"dispatched_at"`
	Outcome      DispatchOutcome     `json:"outcome"`
	IssueContext IssueContextLink    `json:"issue_context"`
}

type DispatchMetadata struct {
	Latest  *DispatchRecord  `json:"latest,omitempty"`
	Records []DispatchRecord `json:"records,omitempty"`
}

type DispatchRequest struct {
	Issue        IssueLocator        `json:"issue"`
	TargetGroup  DispatchTargetGroup `json:"target_group"`
	Terminal     DispatchTerminal    `json:"terminal"`
	IssueContext IssueContextLink    `json:"issue_context,omitempty"`
}

type DispatchResult struct {
	Record DispatchRecord `json:"record"`
}

func NewIssueContextLink(issue Issue, format ContextFormat) IssueContextLink {
	return IssueContextLink{
		SchemaVersion: ContextSchemaVersion,
		Format:        format,
		Provider:      strings.TrimSpace(issue.Provider),
		Repository:    strings.TrimSpace(issue.Repository),
		IssueID:       strings.TrimSpace(issue.ID),
		IssueNumber:   issue.Number,
		HTMLURL:       strings.TrimSpace(issue.HTMLURL),
	}
}

func (t DispatchTerminal) Validate() error {
	switch t.Mode {
	case DispatchTerminalModeReuseExisting:
		if t.Existing == nil {
			return fmt.Errorf("reuse_existing terminal requires existing_terminal")
		}
		if strings.TrimSpace(t.Existing.ID) == "" {
			return fmt.Errorf("reuse_existing terminal requires existing_terminal.id")
		}
		if !t.Existing.RuntimePreserved {
			return fmt.Errorf("reuse_existing terminal must preserve runtime identity")
		}
		if t.New != nil {
			return fmt.Errorf("reuse_existing terminal cannot include new_terminal")
		}
	case DispatchTerminalModeCreateNew:
		if t.New == nil {
			return fmt.Errorf("create_new terminal requires new_terminal")
		}
		if t.Existing != nil {
			return fmt.Errorf("create_new terminal cannot include existing_terminal")
		}
	default:
		return fmt.Errorf("unknown dispatch terminal mode %q", t.Mode)
	}

	return nil
}

func (l IssueContextLink) Validate() error {
	if strings.TrimSpace(l.SchemaVersion) == "" {
		return fmt.Errorf("schema_version is required")
	}
	if l.Format != "" && l.Format != ContextFormatJSON && l.Format != ContextFormatPrompt {
		return fmt.Errorf("unsupported context format %q", l.Format)
	}
	if strings.TrimSpace(l.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if l.IssueNumber <= 0 && strings.TrimSpace(l.IssueID) == "" {
		return fmt.Errorf("issue_context requires issue_number or issue_id")
	}
	return nil
}

func (r DispatchRecord) Validate() error {
	if strings.TrimSpace(r.TargetGroup.ID) == "" {
		return fmt.Errorf("target_group.id is required")
	}
	if err := r.Terminal.Validate(); err != nil {
		return fmt.Errorf("terminal: %w", err)
	}
	if r.DispatchedAt.IsZero() {
		return fmt.Errorf("dispatched_at is required")
	}
	if strings.TrimSpace(string(r.Outcome)) == "" {
		return fmt.Errorf("outcome is required")
	}
	if err := r.IssueContext.Validate(); err != nil {
		return fmt.Errorf("issue_context: %w", err)
	}
	return nil
}

func (m DispatchMetadata) Validate() error {
	if m.Latest != nil {
		if err := m.Latest.Validate(); err != nil {
			return fmt.Errorf("latest: %w", err)
		}
	}
	for i, record := range m.Records {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("records[%d]: %w", i, err)
		}
	}
	return nil
}

func (r DispatchRequest) Validate() error {
	if strings.TrimSpace(r.TargetGroup.ID) == "" {
		return fmt.Errorf("target_group.id is required")
	}
	if r.Issue.Number <= 0 && strings.TrimSpace(r.Issue.ID) == "" {
		return fmt.Errorf("issue locator requires number or id")
	}
	if err := r.Terminal.Validate(); err != nil {
		return fmt.Errorf("terminal: %w", err)
	}
	if !isZeroIssueContextLink(r.IssueContext) {
		if err := r.IssueContext.Validate(); err != nil {
			return fmt.Errorf("issue_context: %w", err)
		}
	}
	return nil
}

func isZeroIssueContextLink(link IssueContextLink) bool {
	return link.SchemaVersion == "" &&
		link.Format == "" &&
		link.Provider == "" &&
		link.Repository == "" &&
		link.IssueID == "" &&
		link.IssueNumber == 0 &&
		link.HTMLURL == ""
}
