package issuecore

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIssueJSONIncludesGitHubShapedFields(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2024, time.January, 2, 12, 30, 0, 0, time.UTC)
	mergedAt := createdAt.Add(2 * time.Hour)
	mergeable := true

	issue := Issue{
		Provider:   ProviderLocal,
		Repository: "bagakit/issues",
		ID:         "issue-42",
		Number:     42,
		Title:      "Scaffold issue core",
		State:      IssueStateOpen,
		User: &Actor{
			Login: "octocat",
			Type:  "User",
		},
		Labels: []Label{
			{Name: "bug", Color: "d73a4a"},
		},
		Reactions: &ReactionRollup{
			TotalCount: 3,
			PlusOne:    2,
			Heart:      1,
		},
		CommentItems: []Comment{
			{
				ID:        "comment-1",
				Body:      "wired through",
				User:      &Actor{Login: "hubot"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
		},
		Timeline: []TimelineEvent{
			{
				Kind:      "cross-referenced",
				CreatedAt: createdAt,
				Payload:   json.RawMessage(`{"source":"pull_request"}`),
			},
		},
		PullRequest: &PullRequestRef{
			Number:         42,
			Repository:     "bagakit/issues",
			HeadRefName:    "feature/t001",
			BaseRefName:    "main",
			State:          PullRequestStateMerged,
			MergedAt:       &mergedAt,
			Mergeable:      &mergeable,
			DiffURL:        "https://example.invalid/pulls/42.diff",
			PatchURL:       "https://example.invalid/pulls/42.patch",
			ReviewDecision: "approved",
		},
		LinkedPullRequests: []PullRequestRef{
			{
				Number:     7,
				Repository: "bagakit/issues",
			},
		},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	raw, err := json.Marshal(issue)
	if err != nil {
		t.Fatalf("marshal issue: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal issue: %v", err)
	}

	if got["provider"] != ProviderLocal {
		t.Fatalf("provider mismatch: got %#v", got["provider"])
	}
	if got["repository"] != "bagakit/issues" {
		t.Fatalf("repository mismatch: got %#v", got["repository"])
	}
	if got["state"] != string(IssueStateOpen) {
		t.Fatalf("state mismatch: got %#v", got["state"])
	}
	if _, ok := got["pull_request"]; !ok {
		t.Fatalf("pull_request field missing")
	}
	if _, ok := got["linked_pull_requests"]; !ok {
		t.Fatalf("linked_pull_requests field missing")
	}
	if _, ok := got["comment_items"]; !ok {
		t.Fatalf("comment_items field missing")
	}
	if _, ok := got["timeline"]; !ok {
		t.Fatalf("timeline field missing")
	}

	reactions, ok := got["reactions"].(map[string]any)
	if !ok {
		t.Fatalf("reactions should be an object: %#v", got["reactions"])
	}
	if _, ok := reactions["+1"]; !ok {
		t.Fatalf("reactions should include +1 rollup: %#v", reactions)
	}
	if _, ok := reactions["heart"]; !ok {
		t.Fatalf("reactions should include heart rollup: %#v", reactions)
	}
}
