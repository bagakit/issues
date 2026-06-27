package scaffold

import (
	"context"

	"github.com/bagakit/issues/pkg/issuecore"
)

type Provider struct {
	descriptor issuecore.ProviderDescriptor
}

func NewLocal() Provider {
	return Provider{
		descriptor: descriptor(issuecore.ProviderLocal, "Local issue provider"),
	}
}

func NewGitHub() Provider {
	return Provider{
		descriptor: descriptor(issuecore.ProviderGitHub, "GitHub issue provider"),
	}
}

func (p Provider) Descriptor() issuecore.ProviderDescriptor {
	return p.descriptor
}

func (p Provider) CreateIssue(context.Context, issuecore.CreateIssueInput) (issuecore.Issue, error) {
	return issuecore.Issue{}, issuecore.NotImplemented(p.descriptor.Name, "create")
}

func (p Provider) ListIssues(context.Context, issuecore.ListIssuesQuery) (issuecore.IssuePage, error) {
	return issuecore.IssuePage{}, issuecore.NotImplemented(p.descriptor.Name, "list")
}

func (p Provider) GetIssue(context.Context, issuecore.IssueLocator) (issuecore.Issue, error) {
	return issuecore.Issue{}, issuecore.NotImplemented(p.descriptor.Name, "get")
}

func (p Provider) UpdateIssue(context.Context, issuecore.IssueLocator, issuecore.IssuePatch) (issuecore.Issue, error) {
	return issuecore.Issue{}, issuecore.NotImplemented(p.descriptor.Name, "update")
}

func (p Provider) AddComment(context.Context, issuecore.IssueLocator, issuecore.AddCommentInput) (issuecore.Comment, error) {
	return issuecore.Comment{}, issuecore.NotImplemented(p.descriptor.Name, "comment")
}

func (p Provider) CloseIssue(context.Context, issuecore.IssueLocator, issuecore.CloseIssueInput) (issuecore.Issue, error) {
	return issuecore.Issue{}, issuecore.NotImplemented(p.descriptor.Name, "close")
}

func (p Provider) ReopenIssue(context.Context, issuecore.IssueLocator, issuecore.ReopenIssueInput) (issuecore.Issue, error) {
	return issuecore.Issue{}, issuecore.NotImplemented(p.descriptor.Name, "reopen")
}

func descriptor(name, title string) issuecore.ProviderDescriptor {
	return issuecore.ProviderDescriptor{
		Name:  name,
		Kind:  name,
		Title: title,
		Stage: issuecore.ProviderStageScaffold,
		Capabilities: issuecore.CapabilitySet{
			Create:  true,
			List:    true,
			Get:     true,
			Update:  true,
			Comment: true,
			Close:   true,
			Reopen:  true,
		},
	}
}
