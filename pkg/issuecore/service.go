package issuecore

import (
	"context"
	"fmt"
	"sort"
)

type Service struct {
	providers map[string]Provider
}

func NewService(providers ...Provider) (*Service, error) {
	svc := &Service{
		providers: map[string]Provider{},
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
