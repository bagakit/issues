package issuecore

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const IssueIDFormatUUIDv7 = "uuidv7"

type IssueID string

func NewIssueID() (IssueID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate issue id: %w", err)
	}
	return IssueID(strings.ToLower(id.String())), nil
}

func ParseIssueID(raw string) (IssueID, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("issue id is required")
	}
	if strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("issue id must not contain leading or trailing whitespace")
	}
	if raw != strings.ToLower(raw) {
		return "", fmt.Errorf("issue id must be lower-case")
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("issue id must be a UUID: %w", err)
	}
	if parsed.Version() != 7 {
		return "", fmt.Errorf("issue id must be UUIDv7")
	}

	canonical := strings.ToLower(parsed.String())
	if canonical != raw {
		return "", fmt.Errorf("issue id must be the canonical lower-case UUIDv7 string")
	}

	return IssueID(canonical), nil
}

func ValidateIssueID(raw string) error {
	_, err := ParseIssueID(raw)
	return err
}

func (id IssueID) String() string {
	return string(id)
}

func (id IssueID) Validate() error {
	return ValidateIssueID(id.String())
}
