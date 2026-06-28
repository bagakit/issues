package issuecore

import "fmt"

const (
	IssueStoreManifestSchemaVersion = "issues.store.manifest.v1"
	IssueStoreProtocolVersion       = "issues.store.protocol.v1"
)

type ShardRule struct {
	Algorithm string `json:"algorithm"`
	Source    string `json:"source"`
	Widths    []int  `json:"widths"`
}

func DefaultIssueShardRule() ShardRule {
	return ShardRule{
		Algorithm: IssueStoreShardAlgorithm,
		Source:    IssueStoreShardSource,
		Widths:    []int{2, 2},
	}
}

func (rule ShardRule) Validate() error {
	if rule.Algorithm != IssueStoreShardAlgorithm {
		return fmt.Errorf("unsupported shard algorithm %q", rule.Algorithm)
	}
	if rule.Source != IssueStoreShardSource {
		return fmt.Errorf("unsupported shard source %q", rule.Source)
	}
	if len(rule.Widths) == 0 {
		return fmt.Errorf("shard widths are required")
	}
	for idx, width := range rule.Widths {
		if width <= 0 {
			return fmt.Errorf("shard width %d must be positive", idx)
		}
	}
	return nil
}

func (rule ShardRule) Segments(id IssueID) ([]string, error) {
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}

	raw := id.String()
	segments := make([]string, 0, len(rule.Widths))
	offset := 0
	for _, width := range rule.Widths {
		if len(raw) < offset+width {
			return nil, fmt.Errorf("issue id %q is too short for shard rule %v", id, rule.Widths)
		}
		segments = append(segments, raw[offset:offset+width])
		offset += width
	}
	return segments, nil
}

type IssueStoreManifest struct {
	SchemaVersion   string    `json:"schema_version"`
	ProtocolVersion string    `json:"protocol_version"`
	RootPrefix      string    `json:"root_prefix"`
	IDFormat        string    `json:"id_format"`
	ShardRule       ShardRule `json:"shard_rule"`
}

func DefaultIssueStoreManifest() IssueStoreManifest {
	return IssueStoreManifest{
		SchemaVersion:   IssueStoreManifestSchemaVersion,
		ProtocolVersion: IssueStoreProtocolVersion,
		RootPrefix:      IssueStoreRootPrefix,
		IDFormat:        IssueIDFormatUUIDv7,
		ShardRule:       DefaultIssueShardRule(),
	}
}

func ValidateIssueStoreManifest(manifest IssueStoreManifest) error {
	if manifest.SchemaVersion != IssueStoreManifestSchemaVersion {
		return fmt.Errorf("manifest schema version must be %q", IssueStoreManifestSchemaVersion)
	}
	if manifest.ProtocolVersion != IssueStoreProtocolVersion {
		return fmt.Errorf("manifest protocol version must be %q", IssueStoreProtocolVersion)
	}
	if manifest.RootPrefix != IssueStoreRootPrefix {
		return fmt.Errorf("manifest root prefix must be %q", IssueStoreRootPrefix)
	}
	if manifest.IDFormat != IssueIDFormatUUIDv7 {
		return fmt.Errorf("manifest id format must be %q", IssueIDFormatUUIDv7)
	}
	if err := manifest.ShardRule.Validate(); err != nil {
		return fmt.Errorf("manifest shard rule is invalid: %w", err)
	}

	defaultRule := DefaultIssueShardRule()
	if manifest.ShardRule.Algorithm != defaultRule.Algorithm ||
		manifest.ShardRule.Source != defaultRule.Source ||
		!sameIntSlice(manifest.ShardRule.Widths, defaultRule.Widths) {
		return fmt.Errorf("manifest shard rule must match %+v", defaultRule)
	}

	return nil
}

func (manifest IssueStoreManifest) Validate() error {
	return ValidateIssueStoreManifest(manifest)
}

func sameIntSlice(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
