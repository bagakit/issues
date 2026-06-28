package issuecore

import (
	"fmt"
	"sort"
	"strings"
)

type IssueRecordSet struct {
	Issue        IssueDocument
	Comments     []CommentDocument
	Timeline     []TimelineEventRecord
	Providers    []ProviderIdentityRecord
	PullRequests *PullRequestLinksRecord
	Dispatch     []DispatchRecordFile
	Extensions   []ExtensionRecord
}

type issueSchemaPathKind string

const (
	issueSchemaPathIssue        issueSchemaPathKind = "issue"
	issueSchemaPathComment      issueSchemaPathKind = "comment"
	issueSchemaPathTimeline     issueSchemaPathKind = "timeline"
	issueSchemaPathProvider     issueSchemaPathKind = "provider"
	issueSchemaPathPullRequests issueSchemaPathKind = "pull_requests"
	issueSchemaPathDispatch     issueSchemaPathKind = "dispatch"
	issueSchemaPathExtension    issueSchemaPathKind = "extension"
)

type parsedIssueSchemaPath struct {
	IssueID IssueID
	Kind    issueSchemaPathKind
	Ordinal int
	Token   string
}

func NewIssueRecordSet(issue Issue) (IssueRecordSet, error) {
	issueID, err := ParseIssueID(issue.ID)
	if err != nil {
		return IssueRecordSet{}, fmt.Errorf("issue id: %w", err)
	}

	set := IssueRecordSet{
		Issue: IssueDocument{
			SchemaVersion:     IssueDocumentSchemaVersion,
			ID:                issueID.String(),
			PrimaryProvider:   issue.Provider,
			Title:             issue.Title,
			State:             issue.State,
			StateReason:       issue.StateReason,
			User:              cloneActor(issue.User),
			AuthorAssociation: issue.AuthorAssociation,
			Labels:            cloneLabels(issue.Labels),
			Milestone:         cloneMilestone(issue.Milestone),
			Assignee:          cloneActor(issue.Assignee),
			Assignees:         cloneActors(issue.Assignees),
			Comments:          issue.Comments,
			Locked:            issue.Locked,
			ActiveLockReason:  issue.ActiveLockReason,
			Reactions:         cloneReactionRollup(issue.Reactions),
			CreatedAt:         issue.CreatedAt,
			UpdatedAt:         issue.UpdatedAt,
			ClosedAt:          cloneTimePtr(issue.ClosedAt),
			ClosedBy:          cloneActor(issue.ClosedBy),
			BodyText:          issue.BodyText,
			Body:              issue.Body,
		},
		Providers: []ProviderIdentityRecord{
			{
				SchemaVersion: ProviderIdentitySchemaVersion,
				IssueID:       issueID.String(),
				Provider:      issue.Provider,
				NodeID:        issue.NodeID,
				Repository:    issue.Repository,
				Number:        issue.Number,
				URL:           issue.URL,
				HTMLURL:       issue.HTMLURL,
				ProviderRaw:   cloneJSONRaw(issue.ProviderRaw),
			},
		},
		PullRequests: &PullRequestLinksRecord{
			SchemaVersion:      PullRequestLinksSchemaVersion,
			IssueID:            issueID.String(),
			PullRequest:        clonePullRequest(issue.PullRequest),
			LinkedPullRequests: clonePullRequests(issue.LinkedPullRequests),
		},
	}

	set.Comments = make([]CommentDocument, 0, len(issue.CommentItems))
	for idx, comment := range issue.CommentItems {
		set.Comments = append(set.Comments, CommentDocument{
			SchemaVersion:     CommentDocumentSchemaVersion,
			IssueID:           issueID.String(),
			Ordinal:           idx + 1,
			ID:                comment.ID,
			NodeID:            comment.NodeID,
			URL:               comment.URL,
			HTMLURL:           comment.HTMLURL,
			User:              cloneActor(comment.User),
			AuthorAssociation: comment.AuthorAssociation,
			CreatedAt:         comment.CreatedAt,
			UpdatedAt:         comment.UpdatedAt,
			Reactions:         cloneReactionRollup(comment.Reactions),
			Pinned:            comment.Pinned,
			PinnedAt:          cloneTimePtr(comment.PinnedAt),
			PinnedBy:          cloneActor(comment.PinnedBy),
			MinimizedReason:   comment.MinimizedReason,
			ProviderRaw:       cloneJSONRaw(comment.ProviderRaw),
			BodyText:          comment.BodyText,
			Body:              comment.Body,
		})
	}

	set.Timeline = make([]TimelineEventRecord, 0, len(issue.Timeline))
	for idx, event := range issue.Timeline {
		set.Timeline = append(set.Timeline, TimelineEventRecord{
			SchemaVersion: TimelineEventSchemaVersion,
			IssueID:       issueID.String(),
			Ordinal:       idx + 1,
			ID:            event.ID,
			NodeID:        event.NodeID,
			Kind:          event.Kind,
			Actor:         cloneActor(event.Actor),
			CreatedAt:     event.CreatedAt,
			Payload:       cloneJSONRaw(event.Payload),
			ProviderRaw:   cloneJSONRaw(event.ProviderRaw),
		})
	}

	set.Dispatch = make([]DispatchRecordFile, 0, len(issueDispatchRecordsForSchema(issue.Dispatch)))
	for idx, record := range issueDispatchRecordsForSchema(issue.Dispatch) {
		set.Dispatch = append(set.Dispatch, DispatchRecordFile{
			SchemaVersion: DispatchRecordFileSchemaVersion,
			IssueID:       issueID.String(),
			Ordinal:       idx + 1,
			Record:        cloneDispatchRecord(&record),
		})
	}

	if err := set.Validate(); err != nil {
		return IssueRecordSet{}, err
	}
	return set, nil
}

func ParseIssueRecordSet(records []LogicalRecord) (IssueRecordSet, error) {
	if len(records) == 0 {
		return IssueRecordSet{}, fmt.Errorf("logical records are required")
	}

	var set IssueRecordSet
	seenPaths := make(map[string]struct{}, len(records))
	seenIssueID := ""

	for idx, record := range records {
		if err := record.Validate(); err != nil {
			return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
		}
		if _, exists := seenPaths[record.Path.String()]; exists {
			return IssueRecordSet{}, fmt.Errorf("records[%d]: duplicate logical path %q", idx, record.Path)
		}
		seenPaths[record.Path.String()] = struct{}{}

		parsedPath, err := parseIssueSchemaPath(record.Path)
		if err != nil {
			return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
		}
		if seenIssueID == "" {
			seenIssueID = parsedPath.IssueID.String()
		} else if seenIssueID != parsedPath.IssueID.String() {
			return IssueRecordSet{}, fmt.Errorf("records[%d]: mixed issue ids %q and %q", idx, seenIssueID, parsedPath.IssueID)
		}

		switch parsedPath.Kind {
		case issueSchemaPathIssue:
			if set.Issue.SchemaVersion != "" {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: duplicate issue document", idx)
			}
			document, err := decodeIssueDocumentRecord(record, parsedPath.IssueID)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.Issue = document
		case issueSchemaPathComment:
			document, err := decodeCommentDocumentRecord(record, parsedPath.IssueID, parsedPath.Ordinal)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.Comments = append(set.Comments, document)
		case issueSchemaPathTimeline:
			event, err := decodeTimelineEventRecord(record, parsedPath.IssueID, parsedPath.Ordinal)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.Timeline = append(set.Timeline, event)
		case issueSchemaPathProvider:
			provider, err := decodeProviderIdentityRecord(record, parsedPath.IssueID, parsedPath.Token)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.Providers = append(set.Providers, provider)
		case issueSchemaPathPullRequests:
			if set.PullRequests != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: duplicate pull request links record", idx)
			}
			links, err := decodePullRequestLinksRecord(record, parsedPath.IssueID)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.PullRequests = &links
		case issueSchemaPathDispatch:
			dispatchRecord, err := decodeDispatchRecordFile(record, parsedPath.IssueID, parsedPath.Ordinal)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.Dispatch = append(set.Dispatch, dispatchRecord)
		case issueSchemaPathExtension:
			extension, err := decodeExtensionRecord(record, parsedPath.IssueID, parsedPath.Token)
			if err != nil {
				return IssueRecordSet{}, fmt.Errorf("records[%d]: %w", idx, err)
			}
			set.Extensions = append(set.Extensions, extension)
		default:
			return IssueRecordSet{}, fmt.Errorf("records[%d]: unsupported schema record path %q", idx, record.Path)
		}
	}

	if err := set.Validate(); err != nil {
		return IssueRecordSet{}, err
	}
	return set, nil
}

func (set IssueRecordSet) Validate() error {
	if err := set.Issue.Validate(); err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	if len(set.Providers) == 0 {
		return fmt.Errorf("at least one provider identity record is required")
	}
	if set.PullRequests == nil {
		return fmt.Errorf("pull request links record is required")
	}

	issueID := set.Issue.ID

	if err := set.PullRequests.Validate(); err != nil {
		return fmt.Errorf("pull_requests: %w", err)
	}
	if set.PullRequests.IssueID != issueID {
		return fmt.Errorf("pull_requests issue_id %q does not match issue id %q", set.PullRequests.IssueID, issueID)
	}

	seenProviders := map[string]struct{}{}
	primaryProviderSeen := set.Issue.PrimaryProvider == ""
	for idx, provider := range set.Providers {
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("providers[%d]: %w", idx, err)
		}
		if provider.IssueID != issueID {
			return fmt.Errorf("providers[%d]: issue_id %q does not match issue id %q", idx, provider.IssueID, issueID)
		}
		if _, exists := seenProviders[provider.Provider]; exists {
			return fmt.Errorf("providers[%d]: duplicate provider %q", idx, provider.Provider)
		}
		seenProviders[provider.Provider] = struct{}{}
		if provider.Provider == set.Issue.PrimaryProvider {
			primaryProviderSeen = true
		}
	}
	if !primaryProviderSeen {
		return fmt.Errorf("issue primary_provider %q has no matching provider identity record", set.Issue.PrimaryProvider)
	}
	if set.Issue.PrimaryProvider == "" && len(set.Providers) > 1 {
		return fmt.Errorf("issue primary_provider is required when multiple provider identity records exist")
	}

	if set.Issue.Comments != len(set.Comments) {
		return fmt.Errorf("issue comment count %d does not match %d comment records", set.Issue.Comments, len(set.Comments))
	}

	if err := validateOrderedSchemaRecords(
		set.Comments,
		"comments",
		issueID,
		func(document CommentDocument) error { return document.Validate() },
		func(document CommentDocument) string { return document.IssueID },
		func(document CommentDocument) int { return document.Ordinal },
		func(document CommentDocument) string { return document.ID },
	); err != nil {
		return err
	}

	if err := validateOrderedSchemaRecords(
		set.Timeline,
		"timeline",
		issueID,
		func(record TimelineEventRecord) error { return record.Validate() },
		func(record TimelineEventRecord) string { return record.IssueID },
		func(record TimelineEventRecord) int { return record.Ordinal },
		func(record TimelineEventRecord) string { return record.ID },
	); err != nil {
		return err
	}

	if err := validateOrderedSchemaRecords(
		set.Dispatch,
		"dispatch",
		issueID,
		func(record DispatchRecordFile) error { return record.Validate() },
		func(record DispatchRecordFile) string { return record.IssueID },
		func(record DispatchRecordFile) int { return record.Ordinal },
		func(record DispatchRecordFile) string { return record.Record.ID },
	); err != nil {
		return err
	}

	seenNamespaces := map[string]struct{}{}
	for idx, extension := range set.Extensions {
		if err := extension.Validate(); err != nil {
			return fmt.Errorf("extensions[%d]: %w", idx, err)
		}
		if extension.IssueID != issueID {
			return fmt.Errorf("extensions[%d]: issue_id %q does not match issue id %q", idx, extension.IssueID, issueID)
		}
		if _, exists := seenNamespaces[extension.Namespace]; exists {
			return fmt.Errorf("extensions[%d]: duplicate namespace %q", idx, extension.Namespace)
		}
		seenNamespaces[extension.Namespace] = struct{}{}
	}

	return nil
}

func (set IssueRecordSet) ToLogicalRecords() ([]LogicalRecord, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}

	issueID, err := ParseIssueID(set.Issue.ID)
	if err != nil {
		return nil, fmt.Errorf("issue id: %w", err)
	}

	records := make([]LogicalRecord, 0, 2+len(set.Providers)+len(set.Comments)+len(set.Timeline)+len(set.Dispatch)+len(set.Extensions))

	issuePath, err := IssueDocumentPath(issueID)
	if err != nil {
		return nil, err
	}
	issueContent, err := set.Issue.MarshalMarkdown()
	if err != nil {
		return nil, err
	}
	records = append(records, LogicalRecord{
		Path:          issuePath,
		MediaType:     schemaMarkdownMediaType,
		SchemaVersion: IssueDocumentSchemaVersion,
		Content:       issueContent,
	})

	for _, comment := range sortCommentDocuments(set.Comments) {
		path, err := CommentDocumentPath(issueID, comment.Ordinal)
		if err != nil {
			return nil, err
		}
		content, err := comment.MarshalMarkdown()
		if err != nil {
			return nil, err
		}
		records = append(records, LogicalRecord{
			Path:          path,
			MediaType:     schemaMarkdownMediaType,
			SchemaVersion: CommentDocumentSchemaVersion,
			Content:       content,
		})
	}

	for _, event := range sortTimelineRecords(set.Timeline) {
		path, err := TimelineEventPath(issueID, event.Ordinal)
		if err != nil {
			return nil, err
		}
		content, err := marshalSchemaJSON(event)
		if err != nil {
			return nil, err
		}
		records = append(records, LogicalRecord{
			Path:          path,
			MediaType:     schemaJSONMediaType,
			SchemaVersion: TimelineEventSchemaVersion,
			Content:       content,
		})
	}

	for _, provider := range sortProviderRecords(set.Providers) {
		path, err := ProviderIdentityPath(issueID, provider.Provider)
		if err != nil {
			return nil, err
		}
		content, err := marshalSchemaJSON(provider)
		if err != nil {
			return nil, err
		}
		records = append(records, LogicalRecord{
			Path:          path,
			MediaType:     schemaJSONMediaType,
			SchemaVersion: ProviderIdentitySchemaVersion,
			Content:       content,
		})
	}

	pullRequestsPath, err := PullRequestLinksPath(issueID)
	if err != nil {
		return nil, err
	}
	pullRequestsContent, err := marshalSchemaJSON(set.PullRequests)
	if err != nil {
		return nil, err
	}
	records = append(records, LogicalRecord{
		Path:          pullRequestsPath,
		MediaType:     schemaJSONMediaType,
		SchemaVersion: PullRequestLinksSchemaVersion,
		Content:       pullRequestsContent,
	})

	for _, dispatchRecord := range sortDispatchRecordFiles(set.Dispatch) {
		path, err := DispatchRecordPath(issueID, dispatchRecord.Ordinal)
		if err != nil {
			return nil, err
		}
		content, err := marshalSchemaJSON(dispatchRecord)
		if err != nil {
			return nil, err
		}
		records = append(records, LogicalRecord{
			Path:          path,
			MediaType:     schemaJSONMediaType,
			SchemaVersion: DispatchRecordFileSchemaVersion,
			Content:       content,
		})
	}

	for _, extension := range sortExtensionRecords(set.Extensions) {
		path, err := ExtensionPath(issueID, extension.Namespace)
		if err != nil {
			return nil, err
		}
		content, err := marshalSchemaJSON(extension)
		if err != nil {
			return nil, err
		}
		records = append(records, LogicalRecord{
			Path:          path,
			MediaType:     schemaJSONMediaType,
			SchemaVersion: ExtensionRecordSchemaVersion,
			Content:       content,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Path.String() < records[j].Path.String()
	})
	return records, nil
}

func (set IssueRecordSet) ToIssue() (Issue, error) {
	if err := set.Validate(); err != nil {
		return Issue{}, err
	}

	issue := Issue{
		ID:                set.Issue.ID,
		Title:             set.Issue.Title,
		Body:              set.Issue.Body,
		BodyText:          set.Issue.BodyText,
		State:             set.Issue.State,
		StateReason:       set.Issue.StateReason,
		User:              cloneActor(set.Issue.User),
		AuthorAssociation: set.Issue.AuthorAssociation,
		Labels:            cloneLabels(set.Issue.Labels),
		Milestone:         cloneMilestone(set.Issue.Milestone),
		Assignee:          cloneActor(set.Issue.Assignee),
		Assignees:         cloneActors(set.Issue.Assignees),
		Comments:          set.Issue.Comments,
		Locked:            set.Issue.Locked,
		ActiveLockReason:  set.Issue.ActiveLockReason,
		Reactions:         cloneReactionRollup(set.Issue.Reactions),
		CreatedAt:         set.Issue.CreatedAt,
		UpdatedAt:         set.Issue.UpdatedAt,
		ClosedAt:          cloneTimePtr(set.Issue.ClosedAt),
		ClosedBy:          cloneActor(set.Issue.ClosedBy),
	}

	providers := sortProviderRecords(set.Providers)
	if len(providers) > 0 {
		provider := providers[0]
		if set.Issue.PrimaryProvider != "" {
			for _, candidate := range providers {
				if candidate.Provider == set.Issue.PrimaryProvider {
					provider = candidate
					break
				}
			}
		}
		issue.Provider = provider.Provider
		issue.Repository = provider.Repository
		issue.NodeID = provider.NodeID
		issue.Number = provider.Number
		issue.URL = provider.URL
		issue.HTMLURL = provider.HTMLURL
		issue.ProviderRaw = cloneJSONRaw(provider.ProviderRaw)
	}

	if set.PullRequests != nil {
		issue.PullRequest = clonePullRequest(set.PullRequests.PullRequest)
		issue.LinkedPullRequests = clonePullRequests(set.PullRequests.LinkedPullRequests)
	}

	comments := sortCommentDocuments(set.Comments)
	issue.CommentItems = make([]Comment, 0, len(comments))
	for _, comment := range comments {
		issue.CommentItems = append(issue.CommentItems, Comment{
			ID:                comment.ID,
			NodeID:            comment.NodeID,
			URL:               comment.URL,
			HTMLURL:           comment.HTMLURL,
			Body:              comment.Body,
			BodyText:          comment.BodyText,
			User:              cloneActor(comment.User),
			AuthorAssociation: comment.AuthorAssociation,
			CreatedAt:         comment.CreatedAt,
			UpdatedAt:         comment.UpdatedAt,
			Reactions:         cloneReactionRollup(comment.Reactions),
			Pinned:            comment.Pinned,
			PinnedAt:          cloneTimePtr(comment.PinnedAt),
			PinnedBy:          cloneActor(comment.PinnedBy),
			MinimizedReason:   comment.MinimizedReason,
			ProviderRaw:       cloneJSONRaw(comment.ProviderRaw),
		})
	}

	timeline := sortTimelineRecords(set.Timeline)
	issue.Timeline = make([]TimelineEvent, 0, len(timeline))
	for _, event := range timeline {
		issue.Timeline = append(issue.Timeline, TimelineEvent{
			ID:          event.ID,
			NodeID:      event.NodeID,
			Kind:        event.Kind,
			Actor:       cloneActor(event.Actor),
			CreatedAt:   event.CreatedAt,
			Payload:     cloneJSONRaw(event.Payload),
			ProviderRaw: cloneJSONRaw(event.ProviderRaw),
		})
	}

	dispatchRecords := sortDispatchRecordFiles(set.Dispatch)
	if len(dispatchRecords) > 0 {
		records := make([]DispatchRecord, 0, len(dispatchRecords))
		for _, dispatchRecord := range dispatchRecords {
			records = append(records, cloneDispatchRecord(&dispatchRecord.Record))
		}
		latest := cloneDispatchRecord(&records[len(records)-1])
		issue.Dispatch = &DispatchMetadata{
			Latest:  &latest,
			Records: records,
		}
	}

	return issue, nil
}

func issueDispatchRecordsForSchema(metadata *DispatchMetadata) []DispatchRecord {
	if metadata == nil {
		return nil
	}
	if len(metadata.Records) > 0 {
		return cloneDispatchRecords(metadata.Records)
	}
	if metadata.Latest != nil {
		record := cloneDispatchRecord(metadata.Latest)
		return []DispatchRecord{record}
	}
	return nil
}

func decodeIssueDocumentRecord(record LogicalRecord, issueID IssueID) (IssueDocument, error) {
	if record.MediaType != schemaMarkdownMediaType {
		return IssueDocument{}, fmt.Errorf("issue document media type must be %q", schemaMarkdownMediaType)
	}
	if record.SchemaVersion != IssueDocumentSchemaVersion {
		return IssueDocument{}, fmt.Errorf("issue document logical record schema version must be %q", IssueDocumentSchemaVersion)
	}

	document, err := ParseIssueDocumentMarkdown(record.Content)
	if err != nil {
		return IssueDocument{}, err
	}
	if document.SchemaVersion != record.SchemaVersion {
		return IssueDocument{}, fmt.Errorf("issue document schema version %q does not match logical record schema version %q", document.SchemaVersion, record.SchemaVersion)
	}
	if document.ID != issueID.String() {
		return IssueDocument{}, fmt.Errorf("issue document id %q does not match path issue id %q", document.ID, issueID)
	}
	return document, nil
}

func decodeCommentDocumentRecord(record LogicalRecord, issueID IssueID, ordinal int) (CommentDocument, error) {
	if record.MediaType != schemaMarkdownMediaType {
		return CommentDocument{}, fmt.Errorf("comment document media type must be %q", schemaMarkdownMediaType)
	}
	if record.SchemaVersion != CommentDocumentSchemaVersion {
		return CommentDocument{}, fmt.Errorf("comment document logical record schema version must be %q", CommentDocumentSchemaVersion)
	}

	document, err := ParseCommentDocumentMarkdown(record.Content)
	if err != nil {
		return CommentDocument{}, err
	}
	if document.SchemaVersion != record.SchemaVersion {
		return CommentDocument{}, fmt.Errorf("comment document schema version %q does not match logical record schema version %q", document.SchemaVersion, record.SchemaVersion)
	}
	if document.IssueID != issueID.String() {
		return CommentDocument{}, fmt.Errorf("comment document issue_id %q does not match path issue id %q", document.IssueID, issueID)
	}
	if document.Ordinal != ordinal {
		return CommentDocument{}, fmt.Errorf("comment document ordinal %06d does not match path ordinal %06d", document.Ordinal, ordinal)
	}
	return document, nil
}

func decodeTimelineEventRecord(record LogicalRecord, issueID IssueID, ordinal int) (TimelineEventRecord, error) {
	if record.MediaType != schemaJSONMediaType {
		return TimelineEventRecord{}, fmt.Errorf("timeline event media type must be %q", schemaJSONMediaType)
	}
	if record.SchemaVersion != TimelineEventSchemaVersion {
		return TimelineEventRecord{}, fmt.Errorf("timeline event logical record schema version must be %q", TimelineEventSchemaVersion)
	}

	var event TimelineEventRecord
	if err := parseSchemaJSON(record.Content, &event); err != nil {
		return TimelineEventRecord{}, err
	}
	if err := event.Validate(); err != nil {
		return TimelineEventRecord{}, err
	}
	if event.SchemaVersion != record.SchemaVersion {
		return TimelineEventRecord{}, fmt.Errorf("timeline event schema version %q does not match logical record schema version %q", event.SchemaVersion, record.SchemaVersion)
	}
	if event.IssueID != issueID.String() {
		return TimelineEventRecord{}, fmt.Errorf("timeline event issue_id %q does not match path issue id %q", event.IssueID, issueID)
	}
	if event.Ordinal != ordinal {
		return TimelineEventRecord{}, fmt.Errorf("timeline event ordinal %06d does not match path ordinal %06d", event.Ordinal, ordinal)
	}
	return event, nil
}

func decodeProviderIdentityRecord(record LogicalRecord, issueID IssueID, providerToken string) (ProviderIdentityRecord, error) {
	if record.MediaType != schemaJSONMediaType {
		return ProviderIdentityRecord{}, fmt.Errorf("provider identity media type must be %q", schemaJSONMediaType)
	}
	if record.SchemaVersion != ProviderIdentitySchemaVersion {
		return ProviderIdentityRecord{}, fmt.Errorf("provider identity logical record schema version must be %q", ProviderIdentitySchemaVersion)
	}

	var provider ProviderIdentityRecord
	if err := parseSchemaJSON(record.Content, &provider); err != nil {
		return ProviderIdentityRecord{}, err
	}
	if err := provider.Validate(); err != nil {
		return ProviderIdentityRecord{}, err
	}
	if provider.SchemaVersion != record.SchemaVersion {
		return ProviderIdentityRecord{}, fmt.Errorf("provider identity schema version %q does not match logical record schema version %q", provider.SchemaVersion, record.SchemaVersion)
	}
	if provider.IssueID != issueID.String() {
		return ProviderIdentityRecord{}, fmt.Errorf("provider identity issue_id %q does not match path issue id %q", provider.IssueID, issueID)
	}
	if provider.Provider != providerToken {
		return ProviderIdentityRecord{}, fmt.Errorf("provider identity provider %q does not match path token %q", provider.Provider, providerToken)
	}
	return provider, nil
}

func decodePullRequestLinksRecord(record LogicalRecord, issueID IssueID) (PullRequestLinksRecord, error) {
	if record.MediaType != schemaJSONMediaType {
		return PullRequestLinksRecord{}, fmt.Errorf("pull request links media type must be %q", schemaJSONMediaType)
	}
	if record.SchemaVersion != PullRequestLinksSchemaVersion {
		return PullRequestLinksRecord{}, fmt.Errorf("pull request links logical record schema version must be %q", PullRequestLinksSchemaVersion)
	}

	var links PullRequestLinksRecord
	if err := parseSchemaJSON(record.Content, &links); err != nil {
		return PullRequestLinksRecord{}, err
	}
	if err := links.Validate(); err != nil {
		return PullRequestLinksRecord{}, err
	}
	if links.SchemaVersion != record.SchemaVersion {
		return PullRequestLinksRecord{}, fmt.Errorf("pull request links schema version %q does not match logical record schema version %q", links.SchemaVersion, record.SchemaVersion)
	}
	if links.IssueID != issueID.String() {
		return PullRequestLinksRecord{}, fmt.Errorf("pull request links issue_id %q does not match path issue id %q", links.IssueID, issueID)
	}
	return links, nil
}

func decodeDispatchRecordFile(record LogicalRecord, issueID IssueID, ordinal int) (DispatchRecordFile, error) {
	if record.MediaType != schemaJSONMediaType {
		return DispatchRecordFile{}, fmt.Errorf("dispatch record media type must be %q", schemaJSONMediaType)
	}
	if record.SchemaVersion != DispatchRecordFileSchemaVersion {
		return DispatchRecordFile{}, fmt.Errorf("dispatch record logical record schema version must be %q", DispatchRecordFileSchemaVersion)
	}

	var dispatchRecord DispatchRecordFile
	if err := parseSchemaJSON(record.Content, &dispatchRecord); err != nil {
		return DispatchRecordFile{}, err
	}
	if err := dispatchRecord.Validate(); err != nil {
		return DispatchRecordFile{}, err
	}
	if dispatchRecord.SchemaVersion != record.SchemaVersion {
		return DispatchRecordFile{}, fmt.Errorf("dispatch record schema version %q does not match logical record schema version %q", dispatchRecord.SchemaVersion, record.SchemaVersion)
	}
	if dispatchRecord.IssueID != issueID.String() {
		return DispatchRecordFile{}, fmt.Errorf("dispatch record issue_id %q does not match path issue id %q", dispatchRecord.IssueID, issueID)
	}
	if dispatchRecord.Ordinal != ordinal {
		return DispatchRecordFile{}, fmt.Errorf("dispatch record ordinal %06d does not match path ordinal %06d", dispatchRecord.Ordinal, ordinal)
	}
	return dispatchRecord, nil
}

func decodeExtensionRecord(record LogicalRecord, issueID IssueID, namespace string) (ExtensionRecord, error) {
	if record.MediaType != schemaJSONMediaType {
		return ExtensionRecord{}, fmt.Errorf("extension media type must be %q", schemaJSONMediaType)
	}
	if record.SchemaVersion != ExtensionRecordSchemaVersion {
		return ExtensionRecord{}, fmt.Errorf("extension logical record schema version must be %q", ExtensionRecordSchemaVersion)
	}

	var extension ExtensionRecord
	if err := parseSchemaJSON(record.Content, &extension); err != nil {
		return ExtensionRecord{}, err
	}
	if err := extension.Validate(); err != nil {
		return ExtensionRecord{}, err
	}
	if extension.SchemaVersion != record.SchemaVersion {
		return ExtensionRecord{}, fmt.Errorf("extension schema version %q does not match logical record schema version %q", extension.SchemaVersion, record.SchemaVersion)
	}
	if extension.IssueID != issueID.String() {
		return ExtensionRecord{}, fmt.Errorf("extension issue_id %q does not match path issue id %q", extension.IssueID, issueID)
	}
	if extension.Namespace != namespace {
		return ExtensionRecord{}, fmt.Errorf("extension namespace %q does not match path namespace %q", extension.Namespace, namespace)
	}
	return extension, nil
}

func parseIssueSchemaPath(path LogicalPath) (parsedIssueSchemaPath, error) {
	if err := ValidateLogicalPath(path); err != nil {
		return parsedIssueSchemaPath{}, err
	}

	segments := strings.Split(path.String(), "/")
	rule := DefaultIssueShardRule()
	baseLen := 3 + len(rule.Widths)
	if len(segments) < baseLen+1 {
		return parsedIssueSchemaPath{}, invalidLogicalPath(path, "path must target an issue schema record")
	}
	if segments[0] != IssueStoreRootPrefix || segments[1] != "by-id" {
		return parsedIssueSchemaPath{}, invalidLogicalPath(path, "path must live under %q", IssueStoreByIDPrefix)
	}

	issueID, err := ParseIssueID(segments[2+len(rule.Widths)])
	if err != nil {
		return parsedIssueSchemaPath{}, invalidLogicalPath(path, "issue id segment is invalid: %v", err)
	}

	basePath, err := IssueDirectoryPath(issueID)
	if err != nil {
		return parsedIssueSchemaPath{}, err
	}
	baseSegments := strings.Split(basePath.String(), "/")
	if len(segments) < len(baseSegments)+1 {
		return parsedIssueSchemaPath{}, invalidLogicalPath(path, "path must include a schema record under %q", basePath)
	}
	for idx := range baseSegments {
		if segments[idx] != baseSegments[idx] {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "path does not match canonical issue directory %q", basePath)
		}
	}

	rest := segments[len(baseSegments):]
	switch {
	case len(rest) == 1 && rest[0] == schemaIssueDocumentName:
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathIssue}, nil
	case len(rest) == 1 && rest[0] == schemaPullRequestsName:
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathPullRequests}, nil
	case len(rest) == 2 && rest[0] == schemaCommentDirectory:
		ordinal, err := parseSchemaOrdinalFile(rest[1], ".md", "comment")
		if err != nil {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "%v", err)
		}
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathComment, Ordinal: ordinal}, nil
	case len(rest) == 2 && rest[0] == schemaTimelineDirectory:
		ordinal, err := parseSchemaOrdinalFile(rest[1], ".json", "timeline")
		if err != nil {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "%v", err)
		}
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathTimeline, Ordinal: ordinal}, nil
	case len(rest) == 2 && rest[0] == schemaDispatchDirectory:
		ordinal, err := parseSchemaOrdinalFile(rest[1], ".json", "dispatch")
		if err != nil {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "%v", err)
		}
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathDispatch, Ordinal: ordinal}, nil
	case len(rest) == 2 && rest[0] == schemaProvidersDirectory:
		token := strings.TrimSuffix(rest[1], ".json")
		if token == rest[1] {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "provider path must end with %q", ".json")
		}
		if err := ValidateProviderToken(token); err != nil {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "%v", err)
		}
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathProvider, Token: token}, nil
	case len(rest) == 2 && rest[0] == schemaExtensionsDir:
		token := strings.TrimSuffix(rest[1], ".json")
		if token == rest[1] {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "extension path must end with %q", ".json")
		}
		if err := ValidateExtensionNamespace(token); err != nil {
			return parsedIssueSchemaPath{}, invalidLogicalPath(path, "%v", err)
		}
		return parsedIssueSchemaPath{IssueID: issueID, Kind: issueSchemaPathExtension, Token: token}, nil
	default:
		return parsedIssueSchemaPath{}, invalidLogicalPath(path, "unsupported canonical issue schema path")
	}
}

func validateOrderedSchemaRecords[T any](
	records []T,
	label string,
	issueID string,
	validate func(T) error,
	recordIssueID func(T) string,
	recordOrdinal func(T) int,
	recordID func(T) string,
) error {
	ordinals := make(map[int]struct{}, len(records))
	ids := make(map[string]struct{}, len(records))
	seenOrdinals := make([]int, 0, len(records))

	for idx, record := range records {
		if err := validate(record); err != nil {
			return fmt.Errorf("%s[%d]: %w", label, idx, err)
		}
		if recordIssueID(record) != issueID {
			return fmt.Errorf("%s[%d]: issue_id %q does not match issue id %q", label, idx, recordIssueID(record), issueID)
		}

		ordinal := recordOrdinal(record)
		if _, exists := ordinals[ordinal]; exists {
			return fmt.Errorf("%s[%d]: duplicate ordinal %06d", label, idx, ordinal)
		}
		ordinals[ordinal] = struct{}{}
		seenOrdinals = append(seenOrdinals, ordinal)

		id := strings.TrimSpace(recordID(record))
		if id == "" {
			continue
		}
		if _, exists := ids[id]; exists {
			return fmt.Errorf("%s[%d]: duplicate id %q", label, idx, id)
		}
		ids[id] = struct{}{}
	}

	sort.Ints(seenOrdinals)
	for idx, ordinal := range seenOrdinals {
		expected := idx + 1
		if ordinal != expected {
			return fmt.Errorf("%s: expected ordinal %06d, found %06d", label, expected, ordinal)
		}
	}

	return nil
}

func sortCommentDocuments(documents []CommentDocument) []CommentDocument {
	sorted := append([]CommentDocument(nil), documents...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Ordinal < sorted[j].Ordinal
	})
	return sorted
}

func sortTimelineRecords(records []TimelineEventRecord) []TimelineEventRecord {
	sorted := append([]TimelineEventRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Ordinal < sorted[j].Ordinal
	})
	return sorted
}

func sortProviderRecords(records []ProviderIdentityRecord) []ProviderIdentityRecord {
	sorted := append([]ProviderIdentityRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Provider < sorted[j].Provider
	})
	return sorted
}

func sortDispatchRecordFiles(records []DispatchRecordFile) []DispatchRecordFile {
	sorted := append([]DispatchRecordFile(nil), records...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Ordinal < sorted[j].Ordinal
	})
	return sorted
}

func sortExtensionRecords(records []ExtensionRecord) []ExtensionRecord {
	sorted := append([]ExtensionRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Namespace < sorted[j].Namespace
	})
	return sorted
}
