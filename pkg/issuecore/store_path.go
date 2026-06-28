package issuecore

import (
	"regexp"
	"strings"
)

const (
	IssueStoreRootPrefix                 = "issues"
	IssueStoreByIDPrefix                 = IssueStoreRootPrefix + "/by-id"
	IssueStoreManifestPath   LogicalPath = IssueStoreRootPrefix + "/manifest.json"
	IssueStoreShardAlgorithm             = "prefix"
	IssueStoreShardSource                = "issue_id"
)

var logicalPathSegmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type LogicalPath string

func (path LogicalPath) String() string {
	return string(path)
}

func ValidateLogicalPath(path LogicalPath) error {
	raw := path.String()
	if raw == "" {
		return invalidLogicalPath(path, "path is required")
	}
	if strings.TrimSpace(raw) != raw {
		return invalidLogicalPath(path, "path must not contain leading or trailing whitespace")
	}
	if strings.HasPrefix(raw, "/") {
		return invalidLogicalPath(path, "path must be relative")
	}
	if strings.HasSuffix(raw, "/") {
		return invalidLogicalPath(path, "path must not end with '/'")
	}
	if strings.Contains(raw, "\\") {
		return invalidLogicalPath(path, "path must use forward slashes")
	}

	segments := strings.Split(raw, "/")
	for _, segment := range segments {
		switch segment {
		case "":
			return invalidLogicalPath(path, "path must not contain empty segments")
		case ".", "..":
			return invalidLogicalPath(path, "path must not contain '.' or '..' segments")
		}
		if !logicalPathSegmentPattern.MatchString(segment) {
			return invalidLogicalPath(path, "segment %q contains unsupported characters", segment)
		}
	}

	return nil
}

func JoinLogicalPath(parts ...string) (LogicalPath, error) {
	if len(parts) == 0 {
		return "", invalidLogicalPath("", "path requires at least one segment")
	}

	path := LogicalPath(strings.Join(parts, "/"))
	if err := ValidateLogicalPath(path); err != nil {
		return "", err
	}
	return path, nil
}

func IssueDirectoryPath(id IssueID) (LogicalPath, error) {
	if err := id.Validate(); err != nil {
		return "", err
	}

	shards, err := DefaultIssueShardRule().Segments(id)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, 3+len(shards))
	parts = append(parts, IssueStoreRootPrefix, "by-id")
	parts = append(parts, shards...)
	parts = append(parts, id.String())
	return JoinLogicalPath(parts...)
}

func JoinIssuePath(id IssueID, parts ...string) (LogicalPath, error) {
	base, err := IssueDirectoryPath(id)
	if err != nil {
		return "", err
	}

	joined := make([]string, 0, 1+len(parts))
	joined = append(joined, base.String())
	joined = append(joined, parts...)
	return JoinLogicalPath(joined...)
}

func ValidateIssuePath(path LogicalPath, id IssueID) error {
	if err := ValidateLogicalPath(path); err != nil {
		return err
	}

	base, err := IssueDirectoryPath(id)
	if err != nil {
		return err
	}

	raw := path.String()
	prefix := base.String()
	if raw == prefix || strings.HasPrefix(raw, prefix+"/") {
		return nil
	}

	return invalidLogicalPath(path, "path does not match canonical issue directory %q", prefix)
}
