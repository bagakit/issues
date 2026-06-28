package issuecore

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidLogicalPath             = errors.New("invalid logical path")
	ErrLogicalRecordNotFound          = errors.New("logical record not found")
	ErrLogicalStoreConflict           = errors.New("logical store conflict")
	ErrLogicalStorePreconditionFailed = errors.New("logical store precondition failed")
)

type LogicalStore interface {
	Read(ctx context.Context, path LogicalPath) (LogicalRecord, error)
	Write(ctx context.Context, record LogicalRecord, expect RecordVersion, createOnly bool) (LogicalRecord, error)
	List(ctx context.Context, req ListRequest) ([]ListEntry, string, error)
}

type LogicalPathError struct {
	Path   LogicalPath
	Reason string
}

func (e *LogicalPathError) Error() string {
	if e == nil {
		return ErrInvalidLogicalPath.Error()
	}
	if e.Path == "" {
		return fmt.Sprintf("%s: %s", ErrInvalidLogicalPath, e.Reason)
	}
	return fmt.Sprintf("%s %q: %s", ErrInvalidLogicalPath, e.Path, e.Reason)
}

func (e *LogicalPathError) Unwrap() error {
	return ErrInvalidLogicalPath
}

func invalidLogicalPath(path LogicalPath, format string, args ...any) error {
	return &LogicalPathError{
		Path:   path,
		Reason: fmt.Sprintf(format, args...),
	}
}

type LogicalStoreError struct {
	Kind   error
	Path   LogicalPath
	Expect RecordVersion
	Actual RecordVersion
	Reason string
}

func (e *LogicalStoreError) Error() string {
	if e == nil || e.Kind == nil {
		return "logical store error"
	}

	switch e.Kind {
	case ErrLogicalRecordNotFound:
		return fmt.Sprintf("%s %q", ErrLogicalRecordNotFound, e.Path)
	case ErrLogicalStoreConflict:
		if e.Reason == "" {
			return fmt.Sprintf("%s %q", ErrLogicalStoreConflict, e.Path)
		}
		return fmt.Sprintf("%s %q: %s", ErrLogicalStoreConflict, e.Path, e.Reason)
	case ErrLogicalStorePreconditionFailed:
		switch {
		case e.Expect != "" && e.Actual != "":
			return fmt.Sprintf("%s %q: expected version %q, got %q", ErrLogicalStorePreconditionFailed, e.Path, e.Expect, e.Actual)
		case e.Expect != "":
			return fmt.Sprintf("%s %q: expected version %q, record is missing", ErrLogicalStorePreconditionFailed, e.Path, e.Expect)
		default:
			return fmt.Sprintf("%s %q", ErrLogicalStorePreconditionFailed, e.Path)
		}
	default:
		if e.Reason == "" {
			return fmt.Sprintf("logical store error %q: %v", e.Path, e.Kind)
		}
		return fmt.Sprintf("logical store error %q: %s: %v", e.Path, e.Reason, e.Kind)
	}
}

func (e *LogicalStoreError) Unwrap() error {
	return e.Kind
}

func LogicalRecordNotFound(path LogicalPath) error {
	return &LogicalStoreError{
		Kind: ErrLogicalRecordNotFound,
		Path: path,
	}
}

func LogicalStoreConflict(path LogicalPath, format string, args ...any) error {
	return &LogicalStoreError{
		Kind:   ErrLogicalStoreConflict,
		Path:   path,
		Reason: fmt.Sprintf(format, args...),
	}
}

func LogicalStorePreconditionFailed(path LogicalPath, expect, actual RecordVersion) error {
	return &LogicalStoreError{
		Kind:   ErrLogicalStorePreconditionFailed,
		Path:   path,
		Expect: expect,
		Actual: actual,
	}
}
