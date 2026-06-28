package issuecore

import (
	"fmt"
	"strings"
)

func NormalizeCloseStateReason(reason IssueStateReason) (IssueStateReason, error) {
	if strings.TrimSpace(string(reason)) == "" {
		reason = IssueStateReasonCompleted
	}

	switch reason {
	case IssueStateReasonCompleted, IssueStateReasonDuplicate, IssueStateReasonNotPlanned:
		return reason, nil
	default:
		return "", fmt.Errorf("unsupported close reason %q", reason)
	}
}

func NormalizeReopenStateReason(reason IssueStateReason) (IssueStateReason, error) {
	if strings.TrimSpace(string(reason)) == "" {
		reason = IssueStateReasonReopened
	}

	if reason != IssueStateReasonReopened {
		return "", fmt.Errorf("unsupported reopen reason %q", reason)
	}
	return reason, nil
}
