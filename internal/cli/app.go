package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	githubprovider "github.com/bagakit/issues/internal/providers/github"
	"github.com/bagakit/issues/internal/providers/local"
	"github.com/bagakit/issues/pkg/issuecore"
)

const (
	modulePath           = "github.com/bagakit/issues"
	localDatabaseEnv     = local.EnvDBPath
	githubTokenEnv       = githubprovider.EnvToken
	githubTokenGHEnv     = githubprovider.EnvTokenGH
	githubTokenCompatEnv = githubprovider.EnvTokenGitHub
	githubAPIBaseURLEnv  = githubprovider.EnvAPIBaseURL
)

type serviceConfig struct {
	LocalDBPath   string
	GitHubToken   string
	GitHubBaseURL string
}

type App struct {
	Version        string
	Service        *issuecore.Service
	ServiceFactory func(serviceConfig) (*issuecore.Service, func(), error)
	Stdout         io.Writer
	Stderr         io.Writer
}

func New(version string) (*App, error) {
	return &App{
		Version: version,
		ServiceFactory: func(cfg serviceConfig) (*issuecore.Service, func(), error) {
			localProvider, err := local.New(local.Config{Path: cfg.LocalDBPath})
			if err != nil {
				return nil, nil, err
			}

			githubProvider, err := githubprovider.New(githubprovider.Config{
				Token:   cfg.GitHubToken,
				BaseURL: cfg.GitHubBaseURL,
			})
			if err != nil {
				_ = localProvider.Close()
				return nil, nil, err
			}

			service, err := issuecore.NewService(
				githubProvider,
				localProvider,
			)
			if err != nil {
				_ = localProvider.Close()
				return nil, nil, err
			}

			return service, func() {
				_ = localProvider.Close()
			}, nil
		},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}, nil
}

func (a *App) Run(ctx context.Context, args []string) int {
	global, args, err := parseGlobalArgs(args)
	if err != nil {
		fmt.Fprintf(a.Stderr, "issues: %v\n", err)
		return 2
	}

	service, cleanup, err := a.resolveService(global)
	if err != nil {
		fmt.Fprintf(a.Stderr, "issues: %v\n", err)
		return 1
	}
	defer cleanup()

	previousService := a.Service
	a.Service = service
	defer func() {
		a.Service = previousService
	}()

	if len(args) == 0 {
		a.writeUsage(a.Stderr)
		return 2
	}

	switch args[0] {
	case "help", "-h", "--help":
		a.writeUsage(a.Stdout)
		return 0
	case "version":
		return a.runVersion(args[1:])
	case "providers":
		return a.runProviders(args[1:])
	case "list":
		return a.runList(ctx, args[1:])
	case "view":
		return a.runView(ctx, args[1:])
	case "create":
		return a.runCreate(ctx, args[1:])
	case "edit":
		return a.runEdit(ctx, args[1:])
	case "context":
		return a.runContext(ctx, args[1:])
	case "comment":
		return a.runComment(ctx, args[1:])
	case "close":
		return a.runClose(ctx, args[1:])
	case "reopen":
		return a.runReopen(ctx, args[1:])
	default:
		fmt.Fprintf(a.Stderr, "issues: unknown command %q\n\n", args[0])
		a.writeUsage(a.Stderr)
		return 2
	}
}

func (a *App) runVersion(args []string) int {
	flags := a.newFlagSet("version")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	result := struct {
		Version   string                         `json:"version"`
		Module    string                         `json:"module"`
		Providers []issuecore.ProviderDescriptor `json:"providers"`
	}{
		Version:   a.version(),
		Module:    modulePath,
		Providers: a.Service.Providers(),
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, result)
	}

	fmt.Fprintf(a.Stdout, "issues %s (%s)\n", result.Version, result.Module)
	for _, descriptor := range result.Providers {
		fmt.Fprintf(a.Stdout, "- %s [%s]\n", descriptor.Name, descriptor.Stage)
	}
	return 0
}

func (a *App) runProviders(args []string) int {
	flags := a.newFlagSet("providers")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	result := struct {
		Providers []issuecore.ProviderDescriptor `json:"providers"`
	}{
		Providers: a.Service.Providers(),
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, result)
	}

	for _, descriptor := range result.Providers {
		fmt.Fprintf(
			a.Stdout,
			"%s\tstage=%s\tops=%s\n",
			descriptor.Name,
			descriptor.Stage,
			strings.Join(capabilities(descriptor.Capabilities), ","),
		)
	}
	return 0
}

func (a *App) runList(ctx context.Context, args []string) int {
	flags := a.newFlagSet("list")
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	state := flags.String("state", string(issuecore.IssueStateFilterOpen), "open|closed|all")
	labels := flags.String("labels", "", "comma-separated labels")
	assignee := flags.String("assignee", "", "assignee login")
	search := flags.String("search", "", "search query")
	limit := flags.Int("limit", 30, "result limit")
	pageToken := flags.String("page-token", "", "opaque provider pagination token")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	query := issuecore.ListIssuesQuery{
		Repository: *repository,
		State:      issuecore.IssueStateFilter(*state),
		Labels:     splitCSV(*labels),
		Assignee:   *assignee,
		Search:     *search,
		Limit:      *limit,
		PageToken:  *pageToken,
	}

	result, err := a.Service.ListIssues(ctx, *provider, query)
	if err != nil {
		return a.renderError(*jsonOut, err)
	}

	payload := struct {
		Provider      string                    `json:"provider"`
		Query         issuecore.ListIssuesQuery `json:"query"`
		Issues        []issuecore.Issue         `json:"issues"`
		NextPageToken string                    `json:"next_page_token,omitempty"`
	}{
		Provider:      *provider,
		Query:         query,
		Issues:        result.Issues,
		NextPageToken: result.NextPageToken,
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, payload)
	}

	fmt.Fprintf(a.Stdout, "%d issues from %s\n", len(result.Issues), *provider)
	return 0
}

func (a *App) runView(ctx context.Context, args []string) int {
	flags := a.newFlagSet("view")
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 1 {
		return a.renderError(*jsonOut, fmt.Errorf("view requires exactly one issue identifier"))
	}

	issue, err := a.Service.GetIssue(ctx, *provider, parseLocator(*repository, flags.Arg(0)))
	if err != nil {
		return a.renderError(*jsonOut, err)
	}

	payload := struct {
		Provider string          `json:"provider"`
		Issue    issuecore.Issue `json:"issue"`
	}{
		Provider: *provider,
		Issue:    issue,
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, payload)
	}

	fmt.Fprintf(a.Stdout, "%s#%d %s\n", issue.Repository, issue.Number, issue.Title)
	return 0
}

func (a *App) runCreate(ctx context.Context, args []string) int {
	flags := a.newFlagSet("create")
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	title := flags.String("title", "", "issue title")
	body := flags.String("body", "", "issue body")
	labels := flags.String("labels", "", "comma-separated labels")
	assignees := flags.String("assignees", "", "comma-separated assignee logins")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if strings.TrimSpace(*title) == "" {
		return a.renderError(*jsonOut, fmt.Errorf("create requires --title"))
	}

	issue, err := a.Service.CreateIssue(ctx, *provider, issuecore.CreateIssueInput{
		Repository: *repository,
		Title:      *title,
		Body:       *body,
		Labels:     splitCSV(*labels),
		Assignees:  splitCSV(*assignees),
	})
	if err != nil {
		return a.renderError(*jsonOut, err)
	}

	payload := struct {
		Provider string          `json:"provider"`
		Issue    issuecore.Issue `json:"issue"`
	}{
		Provider: *provider,
		Issue:    issue,
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, payload)
	}

	fmt.Fprintf(a.Stdout, "created %s#%d %s\n", issue.Repository, issue.Number, issue.Title)
	return 0
}

func (a *App) runEdit(ctx context.Context, args []string) int {
	flags := a.newFlagSet("edit")
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	title := flags.String("title", "", "issue title")
	body := flags.String("body", "", "issue body")
	labels := flags.String("labels", "", "comma-separated labels")
	assignees := flags.String("assignees", "", "comma-separated assignee logins")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 1 {
		return a.renderError(*jsonOut, fmt.Errorf("edit requires exactly one issue identifier"))
	}

	visited := visitedFlags(flags)
	patch := issuecore.IssuePatch{}
	if visited["title"] {
		titleValue := *title
		patch.Title = &titleValue
	}
	if visited["body"] {
		bodyValue := *body
		patch.Body = &bodyValue
	}
	if visited["labels"] {
		labelsValue := splitCSV(*labels)
		patch.Labels = &labelsValue
	}
	if visited["assignees"] {
		assigneesValue := splitCSV(*assignees)
		patch.Assignees = &assigneesValue
	}

	if patch.Title == nil && patch.Body == nil && patch.Labels == nil && patch.Assignees == nil && patch.StateReason == nil {
		return a.renderError(*jsonOut, fmt.Errorf("edit requires at least one field change"))
	}

	issue, err := a.Service.UpdateIssue(ctx, *provider, parseLocator(*repository, flags.Arg(0)), patch)
	if err != nil {
		return a.renderError(*jsonOut, err)
	}

	payload := struct {
		Provider string          `json:"provider"`
		Issue    issuecore.Issue `json:"issue"`
	}{
		Provider: *provider,
		Issue:    issue,
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, payload)
	}

	fmt.Fprintf(a.Stdout, "updated %s#%d %s\n", issue.Repository, issue.Number, issue.Title)
	return 0
}

func (a *App) runContext(ctx context.Context, args []string) int {
	flags := a.newFlagSet("context")
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	defaults := issuecore.DefaultContextOptions()
	bodyMaxRunes := flags.Int("body-max-runes", defaults.BodyMaxRunes, "maximum body runes in rendered context")
	commentMaxRunes := flags.Int("comment-max-runes", defaults.CommentMaxRunes, "maximum comment runes in rendered context")
	timelinePayloadMaxRunes := flags.Int("timeline-payload-max-runes", defaults.TimelinePayloadMaxRunes, "maximum timeline payload preview runes")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 1 {
		return a.renderError(*jsonOut, fmt.Errorf("context requires exactly one issue identifier"))
	}

	options := issuecore.ContextOptions{
		BodyMaxRunes:            *bodyMaxRunes,
		CommentMaxRunes:         *commentMaxRunes,
		TimelinePayloadMaxRunes: *timelinePayloadMaxRunes,
	}
	locator := parseLocator(*repository, flags.Arg(0))

	if *jsonOut {
		rendered, err := a.Service.RenderContext(ctx, *provider, locator, options)
		if err != nil {
			return a.renderError(true, err)
		}
		return a.writeJSON(a.Stdout, rendered)
	}

	prompt, err := a.Service.RenderPrompt(ctx, *provider, locator, options)
	if err != nil {
		return a.renderError(false, err)
	}
	fmt.Fprint(a.Stdout, prompt)
	return 0
}

func (a *App) runComment(ctx context.Context, args []string) int {
	flags := a.newFlagSet("comment")
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	body := flags.String("body", "", "comment body")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 1 {
		return a.renderError(*jsonOut, fmt.Errorf("comment requires exactly one issue identifier"))
	}
	if strings.TrimSpace(*body) == "" {
		return a.renderError(*jsonOut, fmt.Errorf("comment requires --body"))
	}

	comment, err := a.Service.AddComment(ctx, *provider, parseLocator(*repository, flags.Arg(0)), issuecore.AddCommentInput{
		Body: *body,
	})
	if err != nil {
		return a.renderError(*jsonOut, err)
	}

	payload := struct {
		Provider string            `json:"provider"`
		Comment  issuecore.Comment `json:"comment"`
	}{
		Provider: *provider,
		Comment:  comment,
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, payload)
	}

	fmt.Fprintln(a.Stdout, "comment added")
	return 0
}

func (a *App) runClose(ctx context.Context, args []string) int {
	return a.runStateChange(ctx, "close", args)
}

func (a *App) runReopen(ctx context.Context, args []string) int {
	return a.runStateChange(ctx, "reopen", args)
}

func (a *App) runStateChange(ctx context.Context, command string, args []string) int {
	flags := a.newFlagSet(command)
	provider := flags.String("provider", issuecore.ProviderLocal, "provider name")
	repository := flags.String("repository", "", "repository owner/name")
	reason := flags.String("reason", "", "state reason")
	jsonOut := flags.Bool("json", false, "render JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 1 {
		return a.renderError(*jsonOut, fmt.Errorf("%s requires exactly one issue identifier", command))
	}

	locator := parseLocator(*repository, flags.Arg(0))
	reasonValue := issuecore.IssueStateReason(*reason)

	var (
		issue issuecore.Issue
		err   error
	)
	switch command {
	case "close":
		issue, err = a.Service.CloseIssue(ctx, *provider, locator, issuecore.CloseIssueInput{Reason: reasonValue})
	case "reopen":
		issue, err = a.Service.ReopenIssue(ctx, *provider, locator, issuecore.ReopenIssueInput{Reason: reasonValue})
	}
	if err != nil {
		return a.renderError(*jsonOut, err)
	}

	payload := struct {
		Provider string          `json:"provider"`
		Issue    issuecore.Issue `json:"issue"`
	}{
		Provider: *provider,
		Issue:    issue,
	}

	if *jsonOut {
		return a.writeJSON(a.Stdout, payload)
	}

	fmt.Fprintf(a.Stdout, "%s %s#%d\n", command, issue.Repository, issue.Number)
	return 0
}

func (a *App) newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(a.Stderr)
	flags.Usage = func() {}
	return flags
}

func (a *App) renderError(jsonOut bool, err error) int {
	payload := cliError{
		Code:    "internal_error",
		Message: err.Error(),
	}

	var opErr *issuecore.OperationError
	if errors.As(err, &opErr) {
		payload.Code = opErr.Code
		payload.Message = opErr.Error()
		payload.Provider = opErr.Provider
		payload.Operation = opErr.Operation
	}

	if jsonOut {
		if a.writeJSON(a.Stdout, struct {
			Error cliError `json:"error"`
		}{
			Error: payload,
		}) != 0 {
			return 1
		}
		return 1
	}

	fmt.Fprintf(a.Stderr, "issues: %s\n", payload.Message)
	return 1
}

func (a *App) writeJSON(w io.Writer, value any) int {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(a.Stderr, "issues: encode output: %v\n", err)
		return 1
	}
	return 0
}

func (a *App) writeUsage(w io.Writer) {
	fmt.Fprint(w, `issues <command> [flags]

Global Flags:
  --db <path>         local SQLite database path
  `+localDatabaseEnv+`  local SQLite database path environment variable
  --github-token <token>     GitHub provider token
  --github-base-url <url>    GitHub REST API base URL
  `+githubTokenEnv+` / `+githubTokenGHEnv+` / `+githubTokenCompatEnv+`  GitHub provider token environments
  `+githubAPIBaseURLEnv+`    GitHub REST API base URL environment variable

Commands:
  version
  providers
  list
  view <issue>
  create --title <title>
  edit <issue>
  context <issue>
  comment <issue> --body <text>
  close <issue>
  reopen <issue>
`)
}

func (a *App) version() string {
	if strings.TrimSpace(a.Version) == "" {
		return "dev"
	}
	return a.Version
}

type cliError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Provider  string `json:"provider,omitempty"`
	Operation string `json:"operation,omitempty"`
}

func capabilities(set issuecore.CapabilitySet) []string {
	ops := make([]string, 0, 7)
	if set.Create {
		ops = append(ops, "create")
	}
	if set.List {
		ops = append(ops, "list")
	}
	if set.Get {
		ops = append(ops, "get")
	}
	if set.Update {
		ops = append(ops, "update")
	}
	if set.Comment {
		ops = append(ops, "comment")
	}
	if set.Close {
		ops = append(ops, "close")
	}
	if set.Reopen {
		ops = append(ops, "reopen")
	}
	return ops
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseLocator(repository, value string) issuecore.IssueLocator {
	locator := issuecore.IssueLocator{
		Repository: repository,
	}

	if number, err := strconv.Atoi(value); err == nil {
		locator.Number = number
		return locator
	}

	locator.ID = value
	return locator
}

type globalOptions struct {
	LocalDBPath   string
	GitHubToken   string
	GitHubBaseURL string
}

func parseGlobalArgs(args []string) (globalOptions, []string, error) {
	options := globalOptions{
		LocalDBPath: strings.TrimSpace(os.Getenv(localDatabaseEnv)),
		GitHubToken: firstNonEmpty(
			os.Getenv(githubTokenEnv),
			os.Getenv(githubTokenGHEnv),
			os.Getenv(githubTokenCompatEnv),
		),
		GitHubBaseURL: strings.TrimSpace(os.Getenv(githubAPIBaseURLEnv)),
	}

	index := 0
	for index < len(args) {
		arg := args[index]
		switch {
		case arg == "--db" || arg == "--local-db":
			if index+1 >= len(args) {
				return globalOptions{}, nil, fmt.Errorf("%s requires a value", arg)
			}
			options.LocalDBPath = args[index+1]
			index += 2
		case strings.HasPrefix(arg, "--db="):
			options.LocalDBPath = strings.TrimPrefix(arg, "--db=")
			index++
		case strings.HasPrefix(arg, "--local-db="):
			options.LocalDBPath = strings.TrimPrefix(arg, "--local-db=")
			index++
		case arg == "--github-token":
			if index+1 >= len(args) {
				return globalOptions{}, nil, fmt.Errorf("%s requires a value", arg)
			}
			options.GitHubToken = args[index+1]
			index += 2
		case strings.HasPrefix(arg, "--github-token="):
			options.GitHubToken = strings.TrimPrefix(arg, "--github-token=")
			index++
		case arg == "--github-base-url" || arg == "--github-api-url":
			if index+1 >= len(args) {
				return globalOptions{}, nil, fmt.Errorf("%s requires a value", arg)
			}
			options.GitHubBaseURL = args[index+1]
			index += 2
		case strings.HasPrefix(arg, "--github-base-url="):
			options.GitHubBaseURL = strings.TrimPrefix(arg, "--github-base-url=")
			index++
		case strings.HasPrefix(arg, "--github-api-url="):
			options.GitHubBaseURL = strings.TrimPrefix(arg, "--github-api-url=")
			index++
		default:
			return options, args[index:], nil
		}
	}

	return options, nil, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func visitedFlags(flags *flag.FlagSet) map[string]bool {
	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})
	return visited
}

func (a *App) resolveService(options globalOptions) (*issuecore.Service, func(), error) {
	if a.Service != nil {
		return a.Service, func() {}, nil
	}
	if a.ServiceFactory == nil {
		return nil, nil, errors.New("service factory is not configured")
	}
	return a.ServiceFactory(serviceConfig{
		LocalDBPath:   options.LocalDBPath,
		GitHubToken:   options.GitHubToken,
		GitHubBaseURL: options.GitHubBaseURL,
	})
}
