package issuecore

import "testing"

func TestDefaultIssueStoreManifestValidates(t *testing.T) {
	t.Parallel()

	manifest := DefaultIssueStoreManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("default issue store manifest should validate: %v", err)
	}
	if manifest.RootPrefix != IssueStoreRootPrefix {
		t.Fatalf("unexpected root prefix: %q", manifest.RootPrefix)
	}
	if manifest.IDFormat != IssueIDFormatUUIDv7 {
		t.Fatalf("unexpected id format: %q", manifest.IDFormat)
	}
}

func TestDefaultIssueShardRuleSegmentsUseTwoByTwoPrefix(t *testing.T) {
	t.Parallel()

	segments, err := DefaultIssueShardRule().Segments(IssueID(testIssueID))
	if err != nil {
		t.Fatalf("default shard rule segments: %v", err)
	}

	if got, want := len(segments), 2; got != want {
		t.Fatalf("unexpected shard segment count: got %d want %d", got, want)
	}
	if segments[0] != "01" || segments[1] != "8f" {
		t.Fatalf("unexpected shard segments: %#v", segments)
	}
}

func TestIssueStoreManifestRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*IssueStoreManifest)
	}{
		{
			name: "schema-version",
			mutate: func(manifest *IssueStoreManifest) {
				manifest.SchemaVersion = "issues.store.manifest.v0"
			},
		},
		{
			name: "protocol-version",
			mutate: func(manifest *IssueStoreManifest) {
				manifest.ProtocolVersion = "issues.store.protocol.v0"
			},
		},
		{
			name: "root-prefix",
			mutate: func(manifest *IssueStoreManifest) {
				manifest.RootPrefix = "workspace/issues"
			},
		},
		{
			name: "id-format",
			mutate: func(manifest *IssueStoreManifest) {
				manifest.IDFormat = "ulid"
			},
		},
		{
			name: "shard-rule",
			mutate: func(manifest *IssueStoreManifest) {
				manifest.ShardRule.Widths = []int{3, 3}
			},
		},
		{
			name: "shard-source",
			mutate: func(manifest *IssueStoreManifest) {
				manifest.ShardRule.Source = "provider_id"
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manifest := DefaultIssueStoreManifest()
			tc.mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatalf("expected manifest validation error for %s", tc.name)
			}
		})
	}
}
