package issuecore

import (
	"errors"
	"testing"
)

func TestIssueDirectoryPathUsesCanonicalShardRule(t *testing.T) {
	t.Parallel()

	path, err := IssueDirectoryPath(IssueID(testIssueID))
	if err != nil {
		t.Fatalf("issue directory path: %v", err)
	}

	want := LogicalPath("issues/by-id/01/8f/" + testIssueID)
	if path != want {
		t.Fatalf("issue directory path mismatch: got %q want %q", path, want)
	}
}

func TestJoinIssuePathBuildsCanonicalLogicalPath(t *testing.T) {
	t.Parallel()

	path, err := JoinIssuePath(IssueID(testIssueID), "comments", "0001.md")
	if err != nil {
		t.Fatalf("join issue path: %v", err)
	}

	want := LogicalPath("issues/by-id/01/8f/" + testIssueID + "/comments/0001.md")
	if path != want {
		t.Fatalf("issue path mismatch: got %q want %q", path, want)
	}
	if err := ValidateIssuePath(path, IssueID(testIssueID)); err != nil {
		t.Fatalf("joined issue path should validate: %v", err)
	}
}

func TestValidateIssuePathRejectsWrongShardPlacement(t *testing.T) {
	t.Parallel()

	path := LogicalPath("issues/by-id/ff/ff/" + testIssueID + "/issue.md")
	err := ValidateIssuePath(path, IssueID(testIssueID))
	if err == nil {
		t.Fatalf("expected shard validation error")
	}
	if !errors.Is(err, ErrInvalidLogicalPath) {
		t.Fatalf("expected invalid logical path, got %v", err)
	}
}

func TestValidateLogicalPathRejectsInvalidForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path LogicalPath
	}{
		{name: "empty", path: ""},
		{name: "absolute", path: "/issues/manifest.json"},
		{name: "duplicate-slash", path: "issues//manifest.json"},
		{name: "dot-segment", path: "issues/./manifest.json"},
		{name: "space", path: "issues/by id/manifest.json"},
		{name: "backslash", path: "issues\\manifest.json"},
		{name: "upper-case", path: "issues/Manifest.json"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateLogicalPath(tc.path)
			if err == nil {
				t.Fatalf("expected validation error for %q", tc.path)
			}
			if !errors.Is(err, ErrInvalidLogicalPath) {
				t.Fatalf("expected invalid logical path, got %v", err)
			}
		})
	}
}

func TestValidateLogicalPathAcceptsManifestAndIssuePaths(t *testing.T) {
	t.Parallel()

	paths := []LogicalPath{
		IssueStoreManifestPath,
		LogicalPath("issues/by-id/01/8f/" + testIssueID + "/issue.md"),
		LogicalPath("issues/by-id/01/8f/" + testIssueID + "/timeline/events.jsonl"),
	}

	for _, path := range paths {
		if err := ValidateLogicalPath(path); err != nil {
			t.Fatalf("expected path %q to validate: %v", path, err)
		}
	}
}
