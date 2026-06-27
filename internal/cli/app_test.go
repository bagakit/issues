package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bagakit/issues/pkg/issuecore"
)

func TestRunVersionJSONShowsProviders(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := app.Run(context.Background(), []string{"version", "--json"})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}

	var payload struct {
		Version   string `json:"version"`
		Module    string `json:"module"`
		Providers []struct {
			Name  string `json:"name"`
			Stage string `json:"stage"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	if payload.Version != "test-build" {
		t.Fatalf("unexpected version: %q", payload.Version)
	}
	if payload.Module != modulePath {
		t.Fatalf("unexpected module: %q", payload.Module)
	}
	if len(payload.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(payload.Providers))
	}
}

func TestRunGitHubListJSONReturnsStructuredConfigError(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := app.Run(context.Background(), []string{"list", "--provider", "github", "--repository", "bagakit/issues", "--json"})
	if code != 1 {
		t.Fatalf("expected failure exit code 1, got %d with stderr %q", code, stderr.String())
	}

	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Provider  string `json:"provider"`
			Operation string `json:"operation"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	if payload.Error.Code != "provider_config_error" {
		t.Fatalf("unexpected error code: %q", payload.Error.Code)
	}
	if payload.Error.Provider != "github" {
		t.Fatalf("unexpected provider: %q", payload.Error.Provider)
	}
	if payload.Error.Operation != "list" {
		t.Fatalf("unexpected operation: %q", payload.Error.Operation)
	}
}

func TestParseGlobalArgsUsesGitHubEnvFallbacks(t *testing.T) {
	t.Setenv(githubTokenEnv, "issues-token")
	t.Setenv(githubTokenGHEnv, "gh-token")
	t.Setenv(githubTokenCompatEnv, "github-token")
	t.Setenv(githubAPIBaseURLEnv, "https://ghe.example/api/v3")

	options, remaining, err := parseGlobalArgs([]string{"version"})
	if err != nil {
		t.Fatalf("parse global args: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "version" {
		t.Fatalf("unexpected remaining args: %#v", remaining)
	}
	if options.GitHubToken != "issues-token" {
		t.Fatalf("unexpected github token fallback: %q", options.GitHubToken)
	}
	if options.GitHubBaseURL != "https://ghe.example/api/v3" {
		t.Fatalf("unexpected github base url fallback: %q", options.GitHubBaseURL)
	}
}

func TestParseGlobalArgsGitHubFlagsOverrideEnv(t *testing.T) {
	t.Setenv(githubTokenEnv, "issues-token")
	t.Setenv(githubAPIBaseURLEnv, "https://env.example/api/v3")

	options, remaining, err := parseGlobalArgs([]string{
		"--github-token", "flag-token",
		"--github-api-url=https://flag.example/api/v3",
		"version",
	})
	if err != nil {
		t.Fatalf("parse global args: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "version" {
		t.Fatalf("unexpected remaining args: %#v", remaining)
	}
	if options.GitHubToken != "flag-token" {
		t.Fatalf("unexpected github token override: %q", options.GitHubToken)
	}
	if options.GitHubBaseURL != "https://flag.example/api/v3" {
		t.Fatalf("unexpected github base url override: %q", options.GitHubBaseURL)
	}
}

func TestRunLocalProviderWithoutDBPathReturnsStructuredConfigError(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := app.Run(context.Background(), []string{"list", "--json"})
	if code != 1 {
		t.Fatalf("expected failure exit code 1, got %d with stderr %q", code, stderr.String())
	}

	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Provider  string `json:"provider"`
			Operation string `json:"operation"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	if payload.Error.Code != "provider_config_error" {
		t.Fatalf("unexpected error code: %q", payload.Error.Code)
	}
	if payload.Error.Provider != "local" {
		t.Fatalf("unexpected provider: %q", payload.Error.Provider)
	}
	if payload.Error.Operation != "list" {
		t.Fatalf("unexpected operation: %q", payload.Error.Operation)
	}
}

func TestRunLocalProviderSmoke(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	dbPath := filepath.Join(t.TempDir(), "issues.db")

	run := func(args ...string) []byte {
		t.Helper()
		stdout.Reset()
		stderr.Reset()

		argv := append([]string{"--db", dbPath}, args...)
		code := app.Run(context.Background(), argv)
		if code != 0 {
			t.Fatalf("run %v: exit=%d stderr=%q stdout=%q", args, code, stderr.String(), stdout.String())
		}

		return append([]byte(nil), stdout.Bytes()...)
	}

	createOut := run("create", "--title", "T-002 smoke", "--body", "local provider", "--labels", "zeta,alpha", "--assignees", "bob,alice", "--json")
	var createPayload struct {
		Issue struct {
			ID     string `json:"id"`
			Number int    `json:"number"`
			Title  string `json:"title"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
			Assignees []struct {
				Login string `json:"login"`
			} `json:"assignees"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(createOut, &createPayload); err != nil {
		t.Fatalf("decode create output: %v", err)
	}
	if createPayload.Issue.ID == "" || createPayload.Issue.Number != 1 {
		t.Fatalf("unexpected create payload: %+v", createPayload.Issue)
	}
	if got := []string{createPayload.Issue.Labels[0].Name, createPayload.Issue.Labels[1].Name}; got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("labels not normalized: %#v", got)
	}
	if got := []string{createPayload.Issue.Assignees[0].Login, createPayload.Issue.Assignees[1].Login}; got[0] != "alice" || got[1] != "bob" {
		t.Fatalf("assignees not normalized: %#v", got)
	}

	listOut := run("list", "--json")
	var listPayload struct {
		Issues []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(listOut, &listPayload); err != nil {
		t.Fatalf("decode list output: %v", err)
	}
	if len(listPayload.Issues) != 1 || listPayload.Issues[0].Number != 1 {
		t.Fatalf("unexpected list payload: %+v", listPayload.Issues)
	}

	viewOut := run("view", "--json", "1")
	var viewPayload struct {
		Issue struct {
			ID           string `json:"id"`
			Number       int    `json:"number"`
			Title        string `json:"title"`
			Body         string `json:"body"`
			CommentItems []struct {
				ID string `json:"id"`
			} `json:"comment_items"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(viewOut, &viewPayload); err != nil {
		t.Fatalf("decode view output: %v", err)
	}
	if viewPayload.Issue.Number != 1 || viewPayload.Issue.Title != "T-002 smoke" {
		t.Fatalf("unexpected view payload: %+v", viewPayload.Issue)
	}

	editOut := run("edit", "--json", "--title", "T-002 edited", "--labels", "beta,alpha", "--assignees", "alice", "1")
	var editPayload struct {
		Issue struct {
			Title  string `json:"title"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
			Assignees []struct {
				Login string `json:"login"`
			} `json:"assignees"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(editOut, &editPayload); err != nil {
		t.Fatalf("decode edit output: %v", err)
	}
	if editPayload.Issue.Title != "T-002 edited" {
		t.Fatalf("unexpected edit title: %+v", editPayload.Issue)
	}
	if got := []string{editPayload.Issue.Labels[0].Name, editPayload.Issue.Labels[1].Name}; got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("edit labels not normalized: %#v", got)
	}
	if len(editPayload.Issue.Assignees) != 1 || editPayload.Issue.Assignees[0].Login != "alice" {
		t.Fatalf("unexpected assignees after edit: %#v", editPayload.Issue.Assignees)
	}

	commentOut := run("comment", "--json", "--body", "first comment", "1")
	var commentPayload struct {
		Comment struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(commentOut, &commentPayload); err != nil {
		t.Fatalf("decode comment output: %v", err)
	}
	if commentPayload.Comment.ID == "" || commentPayload.Comment.Body != "first comment" {
		t.Fatalf("unexpected comment payload: %+v", commentPayload.Comment)
	}

	closeOut := run("close", "--json", "1")
	var closePayload struct {
		Issue struct {
			State       string `json:"state"`
			StateReason string `json:"state_reason"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(closeOut, &closePayload); err != nil {
		t.Fatalf("decode close output: %v", err)
	}
	if closePayload.Issue.State != "closed" || closePayload.Issue.StateReason != "completed" {
		t.Fatalf("unexpected close payload: %+v", closePayload.Issue)
	}

	reopenOut := run("reopen", "--json", "1")
	var reopenPayload struct {
		Issue struct {
			State        string `json:"state"`
			StateReason  string `json:"state_reason"`
			Comments     int    `json:"comments"`
			CommentItems []struct {
				ID string `json:"id"`
			} `json:"comment_items"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(reopenOut, &reopenPayload); err != nil {
		t.Fatalf("decode reopen output: %v", err)
	}
	if reopenPayload.Issue.State != "open" || reopenPayload.Issue.StateReason != "reopened" {
		t.Fatalf("unexpected reopen payload: %+v", reopenPayload.Issue)
	}
	if reopenPayload.Issue.Comments != 1 || len(reopenPayload.Issue.CommentItems) != 1 {
		t.Fatalf("unexpected reopen comments payload: %+v", reopenPayload.Issue)
	}
}

func TestRunContextJSONIncludesTrustBoundaryAndTruncation(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	dbPath := filepath.Join(t.TempDir(), "issues.db")

	run := func(args ...string) []byte {
		t.Helper()
		stdout.Reset()
		stderr.Reset()

		argv := append([]string{"--db", dbPath}, args...)
		code := app.Run(context.Background(), argv)
		if code != 0 {
			t.Fatalf("run %v: exit=%d stderr=%q stdout=%q", args, code, stderr.String(), stdout.String())
		}

		return append([]byte(nil), stdout.Bytes()...)
	}

	body := "0123456789abcdef"
	comment := "abcdefghijklmno"
	run("create", "--title", "Context JSON", "--body", body, "--json")
	run("comment", "--body", comment, "--json", "1")

	contextOut := run(
		"context",
		"--json",
		"--body-max-runes", "10",
		"--comment-max-runes", "8",
		"--timeline-payload-max-runes", "12",
		"1",
	)

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		TrustBoundary struct {
			ID              string   `json:"id"`
			UntrustedFields []string `json:"untrusted_fields"`
		} `json:"trust_boundary"`
		RenderOptions struct {
			BodyMaxRunes            int `json:"body_max_runes"`
			CommentMaxRunes         int `json:"comment_max_runes"`
			TimelinePayloadMaxRunes int `json:"timeline_payload_max_runes"`
		} `json:"render_options"`
		Issue struct {
			CommentCount int `json:"comment_count"`
			Body         struct {
				Value      string `json:"value"`
				TrustBound string `json:"trust_boundary"`
				Truncation struct {
					Applied       bool `json:"applied"`
					OriginalRunes int  `json:"original_runes"`
					RenderedRunes int  `json:"rendered_runes"`
					OmittedRunes  int  `json:"omitted_runes"`
					LimitRunes    int  `json:"limit_runes"`
				} `json:"truncation"`
			} `json:"body"`
			Comments []struct {
				Body struct {
					Value      string `json:"value"`
					Truncation struct {
						Applied       bool `json:"applied"`
						OriginalRunes int  `json:"original_runes"`
						RenderedRunes int  `json:"rendered_runes"`
						OmittedRunes  int  `json:"omitted_runes"`
						LimitRunes    int  `json:"limit_runes"`
					} `json:"truncation"`
				} `json:"body"`
			} `json:"comments"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(contextOut, &payload); err != nil {
		t.Fatalf("decode context output: %v", err)
	}

	if payload.SchemaVersion != issuecore.ContextSchemaVersion {
		t.Fatalf("unexpected schema version: %q", payload.SchemaVersion)
	}
	if payload.TrustBoundary.ID != issuecore.TrustBoundaryUntrustedUserContent {
		t.Fatalf("unexpected trust boundary: %+v", payload.TrustBoundary)
	}
	if len(payload.TrustBoundary.UntrustedFields) != 2 {
		t.Fatalf("unexpected untrusted fields: %#v", payload.TrustBoundary.UntrustedFields)
	}
	if payload.RenderOptions.BodyMaxRunes != 10 || payload.RenderOptions.CommentMaxRunes != 8 || payload.RenderOptions.TimelinePayloadMaxRunes != 12 {
		t.Fatalf("unexpected render options: %+v", payload.RenderOptions)
	}
	if payload.Issue.CommentCount != 1 || len(payload.Issue.Comments) != 1 {
		t.Fatalf("unexpected comments payload: %+v", payload.Issue)
	}
	if payload.Issue.Body.Value != "0123456789" {
		t.Fatalf("unexpected truncated body: %q", payload.Issue.Body.Value)
	}
	if payload.Issue.Body.TrustBound != issuecore.TrustBoundaryUntrustedUserContent {
		t.Fatalf("unexpected body trust boundary: %q", payload.Issue.Body.TrustBound)
	}
	if !payload.Issue.Body.Truncation.Applied ||
		payload.Issue.Body.Truncation.OriginalRunes != len([]rune(body)) ||
		payload.Issue.Body.Truncation.RenderedRunes != 10 ||
		payload.Issue.Body.Truncation.OmittedRunes != 6 ||
		payload.Issue.Body.Truncation.LimitRunes != 10 {
		t.Fatalf("unexpected body truncation: %+v", payload.Issue.Body.Truncation)
	}
	if payload.Issue.Comments[0].Body.Value != "abcdefgh" {
		t.Fatalf("unexpected truncated comment: %q", payload.Issue.Comments[0].Body.Value)
	}
	if !payload.Issue.Comments[0].Body.Truncation.Applied ||
		payload.Issue.Comments[0].Body.Truncation.OriginalRunes != len([]rune(comment)) ||
		payload.Issue.Comments[0].Body.Truncation.RenderedRunes != 8 ||
		payload.Issue.Comments[0].Body.Truncation.OmittedRunes != 7 ||
		payload.Issue.Comments[0].Body.Truncation.LimitRunes != 8 {
		t.Fatalf("unexpected comment truncation: %+v", payload.Issue.Comments[0].Body.Truncation)
	}

	promptOut := string(run("context", "--body-max-runes", "10", "--comment-max-runes", "8", "1"))
	if !strings.Contains(promptOut, "Trust Boundary:") {
		t.Fatalf("prompt output missing trust boundary: %q", promptOut)
	}
	if !strings.Contains(promptOut, "truncated: showing 10 of 16 runes") {
		t.Fatalf("prompt output missing truncation detail: %q", promptOut)
	}
}
