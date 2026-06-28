package issuecore

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	schemaOrdinalWidth       = 6
	maxSchemaOrdinal         = 999999
	schemaIssueDocumentName  = "issue.md"
	schemaCommentDirectory   = "comments"
	schemaTimelineDirectory  = "timeline"
	schemaProvidersDirectory = "providers"
	schemaPullRequestsName   = "pull-requests.json"
	schemaDispatchDirectory  = "dispatch"
	schemaExtensionsDir      = "extensions"
)

func IssueDocumentPath(id IssueID) (LogicalPath, error) {
	return JoinIssuePath(id, schemaIssueDocumentName)
}

func CommentDocumentPath(id IssueID, ordinal int) (LogicalPath, error) {
	name, err := formatSchemaOrdinalFile(ordinal, ".md", "comment ordinal")
	if err != nil {
		return "", err
	}
	return JoinIssuePath(id, schemaCommentDirectory, name)
}

func TimelineEventPath(id IssueID, ordinal int) (LogicalPath, error) {
	name, err := formatSchemaOrdinalFile(ordinal, ".json", "timeline ordinal")
	if err != nil {
		return "", err
	}
	return JoinIssuePath(id, schemaTimelineDirectory, name)
}

func ProviderIdentityPath(id IssueID, provider string) (LogicalPath, error) {
	if err := ValidateProviderToken(provider); err != nil {
		return "", err
	}
	return JoinIssuePath(id, schemaProvidersDirectory, provider+".json")
}

func PullRequestLinksPath(id IssueID) (LogicalPath, error) {
	return JoinIssuePath(id, schemaPullRequestsName)
}

func DispatchRecordPath(id IssueID, ordinal int) (LogicalPath, error) {
	name, err := formatSchemaOrdinalFile(ordinal, ".json", "dispatch ordinal")
	if err != nil {
		return "", err
	}
	return JoinIssuePath(id, schemaDispatchDirectory, name)
}

func ExtensionPath(id IssueID, namespace string) (LogicalPath, error) {
	if err := ValidateExtensionNamespace(namespace); err != nil {
		return "", err
	}
	return JoinIssuePath(id, schemaExtensionsDir, namespace+".json")
}

func ValidateProviderToken(raw string) error {
	return validateSchemaPathToken(raw, "provider")
}

func ValidateExtensionNamespace(raw string) error {
	return validateSchemaPathToken(raw, "extension namespace")
}

func formatSchemaOrdinalFile(ordinal int, suffix, label string) (string, error) {
	switch {
	case ordinal <= 0:
		return "", fmt.Errorf("%s must be positive", label)
	case ordinal > maxSchemaOrdinal:
		return "", fmt.Errorf("%s must be <= %d", label, maxSchemaOrdinal)
	}

	return fmt.Sprintf("%06d%s", ordinal, suffix), nil
}

func parseSchemaOrdinalFile(name, suffix, label string) (int, error) {
	if !strings.HasSuffix(name, suffix) {
		return 0, fmt.Errorf("%s path must end with %q", label, suffix)
	}

	base := strings.TrimSuffix(name, suffix)
	if len(base) != schemaOrdinalWidth {
		return 0, fmt.Errorf("%s path ordinal must be %d digits", label, schemaOrdinalWidth)
	}
	for _, r := range base {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%s path ordinal must contain only digits", label)
		}
	}

	ordinal, err := strconv.Atoi(base)
	if err != nil {
		return 0, fmt.Errorf("%s path ordinal is invalid: %w", label, err)
	}
	if ordinal <= 0 {
		return 0, fmt.Errorf("%s path ordinal must be positive", label)
	}

	return ordinal, nil
}

func validateSchemaPathToken(raw string, label string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.TrimSpace(raw) != raw {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", label)
	}
	if raw != strings.ToLower(raw) {
		return fmt.Errorf("%s must be lower-case", label)
	}
	if !logicalPathSegmentPattern.MatchString(raw) {
		return fmt.Errorf("%s must match %q", label, logicalPathSegmentPattern.String())
	}

	return nil
}
