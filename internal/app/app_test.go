package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestRunGitHubListJSONReturnsStructuredUnsupportedCapability(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := app.Run(context.Background(), []string{"list", "--provider", "github", "--repository", "bagakit/issues", "--search", "text", "--json"})
	if code != 1 {
		t.Fatalf("expected failure exit code 1, got %d with stderr %q", code, stderr.String())
	}

	var payload struct {
		Error struct {
			Code                  string `json:"code"`
			Provider              string `json:"provider"`
			Operation             string `json:"operation"`
			UnsupportedCapability struct {
				Interface          string `json:"interface"`
				Field              string `json:"field"`
				Behavior           string `json:"behavior"`
				CompatibilityLevel string `json:"compatibility_level"`
				Reason             string `json:"reason"`
			} `json:"unsupported_capability"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	if payload.Error.Code != "unsupported_capability" || payload.Error.Provider != "github" || payload.Error.Operation != "list" {
		t.Fatalf("unexpected error header: %+v", payload.Error)
	}
	if payload.Error.UnsupportedCapability.Interface != "github_rest" ||
		payload.Error.UnsupportedCapability.Field != "search" ||
		payload.Error.UnsupportedCapability.Behavior != "repository_issue_text_search" ||
		payload.Error.UnsupportedCapability.CompatibilityLevel != "unsupported" ||
		payload.Error.UnsupportedCapability.Reason == "" {
		t.Fatalf("unexpected unsupported capability: %+v", payload.Error.UnsupportedCapability)
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

func TestRunInvalidOptionalGitHubBaseURLDoesNotBlockLocalFlows(t *testing.T) {
	t.Setenv(githubAPIBaseURLEnv, "not-a-url")

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	reset := func() {
		stdout.Reset()
		stderr.Reset()
	}

	code := app.Run(context.Background(), []string{"help"})
	if code != 0 {
		t.Fatalf("help should not resolve providers, got exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Commands:") {
		t.Fatalf("help output missing commands: %q", stdout.String())
	}

	reset()
	code = app.Run(context.Background(), []string{"version", "--json"})
	if code != 0 {
		t.Fatalf("version should ignore optional GitHub provider config, got exit=%d stderr=%q", code, stderr.String())
	}
	var versionPayload struct {
		Providers []struct {
			Name string `json:"name"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &versionPayload); err != nil {
		t.Fatalf("decode version output: %v", err)
	}
	if len(versionPayload.Providers) != 1 || versionPayload.Providers[0].Name != issuecore.ProviderLocal {
		t.Fatalf("expected local-only provider set, got %+v", versionPayload.Providers)
	}

	reset()
	localRoot := filepath.Join(t.TempDir(), "issues-root")
	code = app.Run(context.Background(), []string{"--local-root", localRoot, "create", "--title", "local only", "--json"})
	if code != 0 {
		t.Fatalf("local create should ignore optional GitHub provider config, got exit=%d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	reset()
	code = app.Run(context.Background(), []string{"list", "--provider", "github", "--repository", "bagakit/issues", "--json"})
	if code != 1 {
		t.Fatalf("explicit GitHub command should fail provider configuration, got exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "GitHub API base URL") {
		t.Fatalf("expected GitHub base URL error, got stderr=%q stdout=%q", stderr.String(), stdout.String())
	}
}

func TestParseGlobalArgsLocalRootOverridesCompatibilityEnv(t *testing.T) {
	t.Setenv(localStoreEnv, "/tmp/issues-root")
	t.Setenv(localStoreCompatEnv, "/tmp/issues-store")
	t.Setenv(localDatabaseEnv, "/tmp/issues.db")

	options, remaining, err := parseGlobalArgs([]string{"--local-root", "/tmp/flag-root", "version"})
	if err != nil {
		t.Fatalf("parse global args: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "version" {
		t.Fatalf("unexpected remaining args: %#v", remaining)
	}
	if options.LocalStorePath != "/tmp/flag-root" {
		t.Fatalf("unexpected local root override: %q", options.LocalStorePath)
	}

	options, _, err = parseGlobalArgs([]string{"version"})
	if err != nil {
		t.Fatalf("parse env global args: %v", err)
	}
	if options.LocalStorePath != "/tmp/issues-root" {
		t.Fatalf("unexpected local root env precedence: %q", options.LocalStorePath)
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

	createOut := run("create", "--title", "T-002 smoke", "--body", "local provider", "--labels", "zeta,alpha", "--assignees", "bob,alice", "--milestone", "v1", "--json")
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
			Milestone *struct {
				Title string `json:"title"`
			} `json:"milestone"`
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
	if createPayload.Issue.Milestone == nil || createPayload.Issue.Milestone.Title != "v1" {
		t.Fatalf("unexpected milestone: %+v", createPayload.Issue.Milestone)
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

	editOut := run("edit", "--json", "--title", "T-002 edited", "--labels", "beta,alpha", "--assignees", "alice", "--milestone", "v2", "1")
	var editPayload struct {
		Issue struct {
			Title  string `json:"title"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
			Assignees []struct {
				Login string `json:"login"`
			} `json:"assignees"`
			Milestone *struct {
				Title string `json:"title"`
			} `json:"milestone"`
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
	if editPayload.Issue.Milestone == nil || editPayload.Issue.Milestone.Title != "v2" {
		t.Fatalf("unexpected milestone after edit: %+v", editPayload.Issue.Milestone)
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
			Title        string `json:"title"`
			CommentCount int    `json:"comment_count"`
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
			Timeline []struct {
				PayloadPreview string `json:"payload_preview"`
				Truncation     struct {
					Applied       bool `json:"applied"`
					OriginalRunes int  `json:"original_runes"`
					RenderedRunes int  `json:"rendered_runes"`
					OmittedRunes  int  `json:"omitted_runes"`
					LimitRunes    int  `json:"limit_runes"`
				} `json:"payload_preview_truncation"`
			} `json:"timeline"`
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
	if got, want := payload.TrustBoundary.UntrustedFields, []string{
		"issue.title",
		"issue.body.value",
		"issue.comments[].body.value",
		"issue.timeline[].payload",
		"issue.timeline[].payload_preview",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected untrusted fields: got %#v want %#v", got, want)
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
	if len(payload.Issue.Timeline) == 0 || payload.Issue.Timeline[0].PayloadPreview == "" {
		t.Fatalf("expected timeline payload preview, got %+v", payload.Issue.Timeline)
	}
	if !payload.Issue.Timeline[0].Truncation.Applied || payload.Issue.Timeline[0].Truncation.LimitRunes != 12 {
		t.Fatalf("unexpected timeline truncation: %+v", payload.Issue.Timeline[0].Truncation)
	}

	promptOut := string(run("context", "--body-max-runes", "10", "--comment-max-runes", "8", "--timeline-payload-max-runes", "12", "1"))
	if !strings.Contains(promptOut, "Trust Boundary:") {
		t.Fatalf("prompt output missing trust boundary: %q", promptOut)
	}
	if !strings.Contains(promptOut, "Title [trust=untrusted_user_content]:") {
		t.Fatalf("prompt output missing title trust label: %q", promptOut)
	}
	if !strings.Contains(promptOut, "truncated: showing 10 of 16 runes") {
		t.Fatalf("prompt output missing truncation detail: %q", promptOut)
	}
	if !strings.Contains(promptOut, "Payload Preview [trust=untrusted_user_content") {
		t.Fatalf("prompt output missing timeline payload trust label: %q", promptOut)
	}
	if !strings.Contains(promptOut, "Payload Preview [trust=untrusted_user_content, truncated: showing 12 of") {
		t.Fatalf("prompt output missing timeline payload truncation detail: %q", promptOut)
	}
}

func TestRunRecordDispatchPersistsAndContextShowsDispatch(t *testing.T) {
	t.Parallel()

	app, err := New("test-build")
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	localRoot := filepath.Join(t.TempDir(), "issues-root")

	run := func(args ...string) []byte {
		t.Helper()
		stdout.Reset()
		stderr.Reset()

		argv := append([]string{"--local-root", localRoot}, args...)
		code := app.Run(context.Background(), argv)
		if code != 0 {
			t.Fatalf("run %v: exit=%d stderr=%q stdout=%q", args, code, stderr.String(), stdout.String())
		}

		return append([]byte(nil), stdout.Bytes()...)
	}

	createOut := run("create", "--repository", "bagakit/issues", "--title", "dispatch cli", "--body", "dispatch context", "--json")
	var createPayload struct {
		Issue struct {
			ID     string `json:"id"`
			Number int    `json:"number"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(createOut, &createPayload); err != nil {
		t.Fatalf("decode create output: %v", err)
	}
	if createPayload.Issue.ID == "" || createPayload.Issue.Number != 1 {
		t.Fatalf("unexpected create payload: %+v", createPayload.Issue)
	}

	dispatchOut := run(
		"record-dispatch",
		"--repository", "bagakit/issues",
		"--dispatch-id", "dispatch-cli-1",
		"--target-group", "grp-1",
		"--target-group-name", "Spec",
		"--terminal-mode", "reuse_existing",
		"--terminal-id", "term-7",
		"--terminal-title", "Worker 7",
		"--runtime-identity", "codex/gpt-5",
		"--outcome", "delivered",
		"--dispatched-at", "2024-01-02T02:00:00Z",
		"--context-format", "prompt",
		"--json",
		"1",
	)
	var dispatchPayload struct {
		Dispatch struct {
			ID          string `json:"id"`
			TargetGroup struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"target_group"`
			Terminal struct {
				Mode     string `json:"mode"`
				Existing struct {
					ID              string `json:"id"`
					RuntimeIdentity string `json:"runtime_identity"`
				} `json:"existing_terminal"`
			} `json:"terminal"`
			Outcome      string `json:"outcome"`
			IssueContext struct {
				SchemaVersion string `json:"schema_version"`
				Format        string `json:"format"`
				Provider      string `json:"provider"`
				Repository    string `json:"repository"`
				IssueID       string `json:"issue_id"`
				IssueNumber   int    `json:"issue_number"`
			} `json:"issue_context"`
		} `json:"dispatch"`
		Issue struct {
			Dispatch *struct {
				Latest struct {
					ID string `json:"id"`
				} `json:"latest"`
			} `json:"dispatch"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(dispatchOut, &dispatchPayload); err != nil {
		t.Fatalf("decode dispatch output: %v", err)
	}
	if dispatchPayload.Dispatch.ID != "dispatch-cli-1" ||
		dispatchPayload.Dispatch.TargetGroup.ID != "grp-1" ||
		dispatchPayload.Dispatch.Terminal.Mode != "reuse_existing" ||
		dispatchPayload.Dispatch.Terminal.Existing.ID != "term-7" ||
		dispatchPayload.Dispatch.Terminal.Existing.RuntimeIdentity != "codex/gpt-5" ||
		dispatchPayload.Dispatch.Outcome != "delivered" {
		t.Fatalf("unexpected dispatch payload: %+v", dispatchPayload.Dispatch)
	}
	if dispatchPayload.Dispatch.IssueContext.SchemaVersion != issuecore.ContextSchemaVersion ||
		dispatchPayload.Dispatch.IssueContext.Format != "prompt" ||
		dispatchPayload.Dispatch.IssueContext.Provider != issuecore.ProviderLocal ||
		dispatchPayload.Dispatch.IssueContext.Repository != "bagakit/issues" ||
		dispatchPayload.Dispatch.IssueContext.IssueID != createPayload.Issue.ID ||
		dispatchPayload.Dispatch.IssueContext.IssueNumber != 1 {
		t.Fatalf("dispatch issue context was not normalized: %+v", dispatchPayload.Dispatch.IssueContext)
	}
	if dispatchPayload.Issue.Dispatch == nil || dispatchPayload.Issue.Dispatch.Latest.ID != "dispatch-cli-1" {
		t.Fatalf("dispatch output missing issue metadata: %+v", dispatchPayload.Issue.Dispatch)
	}

	issueID, err := issuecore.ParseIssueID(createPayload.Issue.ID)
	if err != nil {
		t.Fatalf("parse issue id: %v", err)
	}
	dispatchPath, err := issuecore.DispatchRecordPath(issueID, 1)
	if err != nil {
		t.Fatalf("dispatch path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localRoot, filepath.FromSlash(dispatchPath.String()))); err != nil {
		t.Fatalf("canonical dispatch record was not written: %v", err)
	}

	viewOut := run("view", "--repository", "bagakit/issues", "--json", "1")
	var viewPayload struct {
		Issue struct {
			Dispatch *struct {
				Latest struct {
					ID string `json:"id"`
				} `json:"latest"`
			} `json:"dispatch"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(viewOut, &viewPayload); err != nil {
		t.Fatalf("decode view output: %v", err)
	}
	if viewPayload.Issue.Dispatch == nil || viewPayload.Issue.Dispatch.Latest.ID != "dispatch-cli-1" {
		t.Fatalf("view output missing dispatch metadata: %+v", viewPayload.Issue.Dispatch)
	}

	contextOut := run("context", "--repository", "bagakit/issues", "--json", "1")
	var contextPayload struct {
		Issue struct {
			Dispatch *struct {
				Latest struct {
					ID       string `json:"id"`
					Terminal struct {
						Existing struct {
							RuntimeIdentity string `json:"runtime_identity"`
						} `json:"existing_terminal"`
					} `json:"terminal"`
				} `json:"latest"`
			} `json:"dispatch"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(contextOut, &contextPayload); err != nil {
		t.Fatalf("decode context output: %v", err)
	}
	if contextPayload.Issue.Dispatch == nil ||
		contextPayload.Issue.Dispatch.Latest.ID != "dispatch-cli-1" ||
		contextPayload.Issue.Dispatch.Latest.Terminal.Existing.RuntimeIdentity != "codex/gpt-5" {
		t.Fatalf("context output missing dispatch metadata: %+v", contextPayload.Issue.Dispatch)
	}

	promptOut := string(run("context", "--repository", "bagakit/issues", "1"))
	for _, want := range []string{
		"Dispatch Records (1):",
		"runtime=codex/gpt-5",
		"context=issues.context.v1/prompt",
	} {
		if !strings.Contains(promptOut, want) {
			t.Fatalf("prompt output missing %q:\n%s", want, promptOut)
		}
	}
}

func TestRunEditAllowsExplicitClearOfBodyLabelsAndAssignees(t *testing.T) {
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

	run("create", "--title", "clear me", "--body", "body", "--labels", "alpha,beta", "--assignees", "alice,bob", "--json")

	editOut := run("edit", "--json", "--body", "", "--labels", "", "--assignees", "", "1")
	var editPayload struct {
		Issue struct {
			Body   string `json:"body"`
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
	if editPayload.Issue.Body != "" {
		t.Fatalf("expected empty body after clear, got %q", editPayload.Issue.Body)
	}
	if len(editPayload.Issue.Labels) != 0 {
		t.Fatalf("expected labels cleared, got %+v", editPayload.Issue.Labels)
	}
	if len(editPayload.Issue.Assignees) != 0 {
		t.Fatalf("expected assignees cleared, got %+v", editPayload.Issue.Assignees)
	}

	viewOut := run("view", "--json", "1")
	var viewPayload struct {
		Issue struct {
			Body   string `json:"body"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
			Assignees []struct {
				Login string `json:"login"`
			} `json:"assignees"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(viewOut, &viewPayload); err != nil {
		t.Fatalf("decode view output: %v", err)
	}
	if viewPayload.Issue.Body != "" || len(viewPayload.Issue.Labels) != 0 || len(viewPayload.Issue.Assignees) != 0 {
		t.Fatalf("clear did not persist: %+v", viewPayload.Issue)
	}
}

func TestRunEditDoesNotExposeStateReasonFlag(t *testing.T) {
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
	code := app.Run(context.Background(), []string{
		"--db", dbPath,
		"edit",
		"--state-reason", "completed",
		"1",
	})
	if code != 2 {
		t.Fatalf("expected flag parse failure, got %d with stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -state-reason") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunEditRejectsExplicitEmptyTitle(t *testing.T) {
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

	run("create", "--title", "reject empty title", "--json")

	stdout.Reset()
	stderr.Reset()
	code := app.Run(context.Background(), []string{"--db", dbPath, "edit", "--json", "--title", "", "1"})
	if code != 1 {
		t.Fatalf("expected edit failure, got %d with stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Provider  string `json:"provider"`
			Operation string `json:"operation"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode error output: %v", err)
	}
	if payload.Error.Code != "invalid_argument" || payload.Error.Provider != issuecore.ProviderLocal || payload.Error.Operation != "update" {
		t.Fatalf("unexpected error payload: %+v", payload.Error)
	}
	if !strings.Contains(payload.Error.Message, "issue title cannot be empty") {
		t.Fatalf("unexpected error message: %q", payload.Error.Message)
	}
}

func TestRunReopenNoOpPreservesUpdatedAtAndTimeline(t *testing.T) {
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

	createOut := run("create", "--title", "reopen noop", "--json")
	var createPayload struct {
		Issue struct {
			UpdatedAt string `json:"updated_at"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(createOut, &createPayload); err != nil {
		t.Fatalf("decode create output: %v", err)
	}

	reopenOut := run("reopen", "--json", "1")
	var reopenPayload struct {
		Issue struct {
			State       string  `json:"state"`
			StateReason string  `json:"state_reason"`
			UpdatedAt   string  `json:"updated_at"`
			ClosedAt    *string `json:"closed_at"`
			Timeline    []struct {
				Kind string `json:"kind"`
			} `json:"timeline"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(reopenOut, &reopenPayload); err != nil {
		t.Fatalf("decode reopen output: %v", err)
	}
	if reopenPayload.Issue.State != "open" || reopenPayload.Issue.StateReason != "" {
		t.Fatalf("unexpected reopen issue: %+v", reopenPayload.Issue)
	}
	if reopenPayload.Issue.UpdatedAt != createPayload.Issue.UpdatedAt {
		t.Fatalf("updated_at changed on reopen no-op: before=%q after=%q", createPayload.Issue.UpdatedAt, reopenPayload.Issue.UpdatedAt)
	}
	if reopenPayload.Issue.ClosedAt != nil {
		t.Fatalf("closed_at should stay nil on reopen no-op: %+v", reopenPayload.Issue.ClosedAt)
	}
	if got := len(reopenPayload.Issue.Timeline); got != 1 || reopenPayload.Issue.Timeline[0].Kind != "created" {
		t.Fatalf("unexpected timeline after reopen no-op: %+v", reopenPayload.Issue.Timeline)
	}
}
