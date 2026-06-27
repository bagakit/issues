package issuecore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDispatchRecordValidateAndJSONShape(t *testing.T) {
	t.Parallel()

	issue := Issue{
		Provider:   ProviderLocal,
		Repository: "bagakit/issues",
		Number:     11,
		HTMLURL:    "https://example.invalid/issues/11",
	}

	record := DispatchRecord{
		ID:          "dispatch-1",
		TargetGroup: DispatchTargetGroup{ID: "grp-1", Name: "Spec"},
		Terminal: DispatchTerminal{
			Mode: DispatchTerminalModeCreateNew,
			New: &NewTerminal{
				Title: "Spec Terminal",
				Runtime: &RuntimeSelection{
					Agent:   "codex",
					Runtime: "gpt-5",
				},
			},
		},
		DispatchedAt: time.Date(2024, time.January, 2, 4, 0, 0, 0, time.UTC),
		Outcome:      DispatchOutcomeDelivered,
		IssueContext: NewIssueContextLink(issue, ContextFormatJSON),
	}

	if err := record.Validate(); err != nil {
		t.Fatalf("record should validate: %v", err)
	}

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if _, ok := got["target_group"]; !ok {
		t.Fatalf("target_group missing from json: %#v", got)
	}
	terminal, ok := got["terminal"].(map[string]any)
	if !ok {
		t.Fatalf("terminal missing from json: %#v", got["terminal"])
	}
	if _, ok := terminal["new_terminal"]; !ok {
		t.Fatalf("new_terminal missing from json: %#v", terminal)
	}
	if _, ok := got["issue_context"]; !ok {
		t.Fatalf("issue_context missing from json: %#v", got)
	}
}

func TestDispatchRecordValidateRejectsReuseWithoutRuntimePreservation(t *testing.T) {
	t.Parallel()

	record := DispatchRecord{
		TargetGroup: DispatchTargetGroup{ID: "grp-2"},
		Terminal: DispatchTerminal{
			Mode: DispatchTerminalModeReuseExisting,
			Existing: &ExistingTerminal{
				ID:               "term-2",
				RuntimePreserved: false,
			},
		},
		DispatchedAt: time.Date(2024, time.January, 2, 5, 0, 0, 0, time.UTC),
		Outcome:      DispatchOutcomeDelivered,
		IssueContext: IssueContextLink{
			SchemaVersion: ContextSchemaVersion,
			Format:        ContextFormatPrompt,
			Provider:      ProviderLocal,
			IssueNumber:   2,
		},
	}

	err := record.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "preserve runtime identity") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
