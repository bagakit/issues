package issuecore

import (
	"fmt"
	"strings"
)

type RecordVersion string

func (version RecordVersion) String() string {
	return string(version)
}

type LogicalRecord struct {
	Path          LogicalPath   `json:"path"`
	MediaType     string        `json:"media_type"`
	SchemaVersion string        `json:"schema_version"`
	Content       []byte        `json:"content"`
	Version       RecordVersion `json:"version,omitempty"`
}

func (record LogicalRecord) Validate() error {
	if err := ValidateLogicalPath(record.Path); err != nil {
		return err
	}
	if strings.TrimSpace(record.MediaType) == "" {
		return fmt.Errorf("logical record media type is required")
	}
	if strings.TrimSpace(record.SchemaVersion) == "" {
		return fmt.Errorf("logical record schema version is required")
	}
	return nil
}

type ListRequest struct {
	Prefix LogicalPath `json:"prefix,omitempty"`
	Cursor string      `json:"cursor,omitempty"`
	Limit  int         `json:"limit,omitempty"`
}

func (req ListRequest) Validate() error {
	if req.Prefix != "" {
		if err := ValidateLogicalPath(req.Prefix); err != nil {
			return err
		}
	}
	if req.Limit < 0 {
		return fmt.Errorf("list limit must be non-negative")
	}
	return nil
}

type ListEntry struct {
	Path          LogicalPath   `json:"path"`
	MediaType     string        `json:"media_type"`
	SchemaVersion string        `json:"schema_version"`
	Version       RecordVersion `json:"version,omitempty"`
	SizeBytes     int64         `json:"size_bytes"`
}

func (entry ListEntry) Validate() error {
	if err := ValidateLogicalPath(entry.Path); err != nil {
		return err
	}
	if strings.TrimSpace(entry.MediaType) == "" {
		return fmt.Errorf("list entry media type is required")
	}
	if strings.TrimSpace(entry.SchemaVersion) == "" {
		return fmt.Errorf("list entry schema version is required")
	}
	if entry.SizeBytes < 0 {
		return fmt.Errorf("list entry size must be non-negative")
	}
	return nil
}
