package issuecore

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testIssueID = "018f6c57-8c2d-7a60-91ad-8461d3f6be11"

func TestNewIssueIDReturnsCanonicalLowerCaseUUIDv7(t *testing.T) {
	t.Parallel()

	id, err := NewIssueID()
	if err != nil {
		t.Fatalf("new issue id: %v", err)
	}

	if err := id.Validate(); err != nil {
		t.Fatalf("generated id should validate: %v", err)
	}
	if id.String() != strings.ToLower(id.String()) {
		t.Fatalf("generated id should be lower-case: %q", id)
	}

	parsed, err := uuid.Parse(id.String())
	if err != nil {
		t.Fatalf("parse generated id: %v", err)
	}
	if parsed.Version() != 7 {
		t.Fatalf("generated id should be UUIDv7, got v%d", parsed.Version())
	}
}

func TestParseIssueIDAcceptsCanonicalString(t *testing.T) {
	t.Parallel()

	id, err := ParseIssueID(testIssueID)
	if err != nil {
		t.Fatalf("parse issue id: %v", err)
	}
	if got := id.String(); got != testIssueID {
		t.Fatalf("parsed issue id mismatch: got %q want %q", got, testIssueID)
	}
}

func TestValidateIssueIDRejectsNonCanonicalValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: " " + testIssueID + " "},
		{name: "upper-case", value: strings.ToUpper(testIssueID)},
		{name: "wrong-version", value: "2f6c7c9f-3b0d-4a7b-8c1d-9e0f12345678"},
		{name: "provider-like", value: "bagakit/issues#7"},
		{name: "url", value: "https://example.invalid/issues/7"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateIssueID(tc.value); err == nil {
				t.Fatalf("expected validation error for %q", tc.value)
			}
		})
	}
}
