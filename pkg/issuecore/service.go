package issuecore

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Service struct {
	providers map[string]Provider
	dispatch  DispatchGateway
}

func NewService(providers ...Provider) (*Service, error) {
	return NewServiceWithDispatch(nil, providers...)
}

func NewServiceWithDispatch(dispatch DispatchGateway, providers ...Provider) (*Service, error) {
	svc := &Service{
		providers: map[string]Provider{},
		dispatch:  dispatch,
	}

	for _, provider := range providers {
		if err := svc.Register(provider); err != nil {
			return nil, err
		}
	}

	return svc, nil
}

func (s *Service) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("provider is nil")
	}

	descriptor := provider.Descriptor()
	if descriptor.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	if _, exists := s.providers[descriptor.Name]; exists {
		return fmt.Errorf("%w: %s", ErrProviderAlreadyRegistered, descriptor.Name)
	}

	s.providers[descriptor.Name] = provider
	return nil
}

func (s *Service) Providers() []ProviderDescriptor {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	sort.Strings(names)

	descriptors := make([]ProviderDescriptor, 0, len(names))
	for _, name := range names {
		descriptors = append(descriptors, s.providers[name].Descriptor())
	}
	return descriptors
}

func (s *Service) CreateIssue(ctx context.Context, provider string, input CreateIssueInput) (Issue, error) {
	backend, err := s.provider(provider, "create")
	if err != nil {
		return Issue{}, err
	}

	issue, err := backend.CreateIssue(ctx, input)
	if err != nil {
		return Issue{}, err
	}
	return normalizeIssue(issue, provider), nil
}

func (s *Service) ListIssues(ctx context.Context, provider string, query ListIssuesQuery) (IssuePage, error) {
	backend, err := s.provider(provider, "list")
	if err != nil {
		return IssuePage{}, err
	}

	page, err := backend.ListIssues(ctx, query)
	if err != nil {
		return IssuePage{}, err
	}

	for i := range page.Issues {
		page.Issues[i] = normalizeIssue(page.Issues[i], provider)
	}
	return page, nil
}

func (s *Service) GetIssue(ctx context.Context, provider string, locator IssueLocator) (Issue, error) {
	backend, err := s.provider(provider, "get")
	if err != nil {
		return Issue{}, err
	}

	issue, err := backend.GetIssue(ctx, withProvider(locator, provider))
	if err != nil {
		return Issue{}, err
	}
	return normalizeIssue(issue, provider), nil
}

func (s *Service) UpdateIssue(ctx context.Context, provider string, locator IssueLocator, patch IssuePatch) (Issue, error) {
	backend, err := s.provider(provider, "update")
	if err != nil {
		return Issue{}, err
	}

	issue, err := backend.UpdateIssue(ctx, withProvider(locator, provider), patch)
	if err != nil {
		return Issue{}, err
	}
	return normalizeIssue(issue, provider), nil
}

func (s *Service) AddComment(ctx context.Context, provider string, locator IssueLocator, input AddCommentInput) (Comment, error) {
	backend, err := s.provider(provider, "comment")
	if err != nil {
		return Comment{}, err
	}

	return backend.AddComment(ctx, withProvider(locator, provider), input)
}

func (s *Service) CloseIssue(ctx context.Context, provider string, locator IssueLocator, input CloseIssueInput) (Issue, error) {
	backend, err := s.provider(provider, "close")
	if err != nil {
		return Issue{}, err
	}

	issue, err := backend.CloseIssue(ctx, withProvider(locator, provider), input)
	if err != nil {
		return Issue{}, err
	}
	return normalizeIssue(issue, provider), nil
}

func (s *Service) ReopenIssue(ctx context.Context, provider string, locator IssueLocator, input ReopenIssueInput) (Issue, error) {
	backend, err := s.provider(provider, "reopen")
	if err != nil {
		return Issue{}, err
	}

	issue, err := backend.ReopenIssue(ctx, withProvider(locator, provider), input)
	if err != nil {
		return Issue{}, err
	}
	return normalizeIssue(issue, provider), nil
}

func (s *Service) RenderContext(ctx context.Context, provider string, locator IssueLocator, options ContextOptions) (IssueContext, error) {
	issue, err := s.GetIssue(ctx, provider, locator)
	if err != nil {
		return IssueContext{}, err
	}
	return RenderIssueContext(issue, options), nil
}

func (s *Service) RenderPrompt(ctx context.Context, provider string, locator IssueLocator, options ContextOptions) (string, error) {
	issue, err := s.GetIssue(ctx, provider, locator)
	if err != nil {
		return "", err
	}
	return RenderIssuePrompt(issue, options), nil
}

func (s *Service) ListDispatchTargets(ctx context.Context, provider string, locator IssueLocator) ([]DispatchTargetGroup, error) {
	if s.dispatch == nil {
		return nil, fmt.Errorf("dispatch gateway is not configured")
	}

	backend, err := s.provider(provider, "dispatch_targets")
	if err != nil {
		return nil, err
	}

	issue, err := backend.GetIssue(ctx, withProvider(locator, provider))
	if err != nil {
		return nil, err
	}

	return s.dispatch.ListDispatchTargets(ctx, normalizeIssue(issue, provider))
}

func (s *Service) SubmitDispatch(ctx context.Context, provider string, request DispatchRequest) (DispatchResult, error) {
	if s.dispatch == nil {
		return DispatchResult{}, fmt.Errorf("dispatch gateway is not configured")
	}

	backend, err := s.provider(provider, "dispatch")
	if err != nil {
		return DispatchResult{}, err
	}

	issue, err := backend.GetIssue(ctx, withProvider(request.Issue, provider))
	if err != nil {
		return DispatchResult{}, err
	}
	issue = normalizeIssue(issue, provider)

	request = normalizeDispatchRequest(issue, request)
	if err := request.Validate(); err != nil {
		return DispatchResult{}, InvalidInput(provider, "dispatch", err)
	}
	result, err := s.dispatch.SubmitDispatch(ctx, issue, request)
	if err != nil {
		return DispatchResult{}, err
	}

	result, err = normalizeDispatchResult(request, result)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("dispatch result is invalid: %w", err)
	}

	if recorder, ok := backend.(DispatchRecorder); ok {
		if _, err := recorder.RecordDispatch(ctx, request.Issue, result.Record); err != nil {
			return result, PostDeliveryPersistence(provider, "dispatch", result, err)
		}
	}

	return result, nil
}

func (s *Service) RecordDispatch(ctx context.Context, provider string, locator IssueLocator, record DispatchRecord) (Issue, error) {
	backend, err := s.provider(provider, "dispatch")
	if err != nil {
		return Issue{}, err
	}

	recorder, ok := backend.(DispatchRecorder)
	if !ok {
		return Issue{}, NotImplemented(provider, "dispatch")
	}

	issue, err := backend.GetIssue(ctx, withProvider(locator, provider))
	if err != nil {
		return Issue{}, err
	}
	issue = normalizeIssue(issue, provider)
	record = normalizeDispatchRecord(issue, record)
	if err := record.Validate(); err != nil {
		return Issue{}, InvalidInput(provider, "dispatch", err)
	}

	recorded, err := recorder.RecordDispatch(ctx, dispatchIssueLocator(issue), record)
	if err != nil {
		return Issue{}, err
	}
	return normalizeIssue(recorded, provider), nil
}

func (s *Service) provider(name, operation string) (Provider, error) {
	if name == "" {
		return nil, ProviderRequiredError(operation)
	}

	backend, exists := s.providers[name]
	if !exists {
		return nil, ProviderLookupError(name, operation)
	}
	return backend, nil
}

func withProvider(locator IssueLocator, provider string) IssueLocator {
	if locator.Provider == "" {
		locator.Provider = provider
	}
	return locator
}

func normalizeIssue(issue Issue, provider string) Issue {
	if issue.Provider == "" {
		issue.Provider = provider
	}
	return issue
}

func normalizeDispatchRequest(issue Issue, request DispatchRequest) DispatchRequest {
	request.Issue = dispatchIssueLocator(issue)
	request.IssueContext = mergeIssueContextLink(NewIssueContextLink(issue, ContextFormatJSON), request.IssueContext)
	return request
}

func normalizeDispatchRecord(issue Issue, record DispatchRecord) DispatchRecord {
	record.IssueContext = mergeIssueContextLink(NewIssueContextLink(issue, ContextFormatJSON), record.IssueContext)
	return record
}

func normalizeDispatchResult(request DispatchRequest, result DispatchResult) (DispatchResult, error) {
	result.Record.TargetGroup = mergeDispatchTargetGroup(request.TargetGroup, result.Record.TargetGroup)
	result.Record.Terminal = mergeDispatchTerminal(request.Terminal, result.Record.Terminal)
	result.Record.IssueContext = mergeIssueContextLink(request.IssueContext, result.Record.IssueContext)
	if err := result.Record.Validate(); err != nil {
		return DispatchResult{}, err
	}
	return result, nil
}

func mergeIssueContextLink(base, override IssueContextLink) IssueContextLink {
	if strings.TrimSpace(override.SchemaVersion) != "" {
		base.SchemaVersion = override.SchemaVersion
	}
	if override.Format != "" {
		base.Format = override.Format
	}
	return base
}

func dispatchIssueLocator(issue Issue) IssueLocator {
	return IssueLocator{
		Provider:   strings.TrimSpace(issue.Provider),
		Repository: strings.TrimSpace(issue.Repository),
		ID:         strings.TrimSpace(issue.ID),
		Number:     issue.Number,
	}
}

func mergeDispatchTargetGroup(base, override DispatchTargetGroup) DispatchTargetGroup {
	if strings.TrimSpace(override.ID) == "" {
		override.ID = base.ID
	}
	if strings.TrimSpace(override.Name) == "" {
		override.Name = base.Name
	}
	if len(override.Terminals) == 0 {
		override.Terminals = base.Terminals
	}
	return override
}

func mergeDispatchTerminal(base, override DispatchTerminal) DispatchTerminal {
	if override.Mode == "" {
		return base
	}
	if override.Existing == nil {
		override.Existing = base.Existing
	}
	if override.New == nil {
		override.New = base.New
	}
	return override
}
