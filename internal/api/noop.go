package api

import (
	"context"

	"github.com/mudrii/golink/internal/output"
)

// NoopTransport is the fallback used when a real adapter has not been wired
// for the requested transport (e.g. --transport=unofficial without an
// implementation). Every method returns ErrFeatureUnavailable carrying a
// stable feature identifier so callers can render a status:"unsupported"
// envelope.
type NoopTransport struct {
	transportName     string
	suggestedFallback string
}

// NewNoopTransport constructs a NoopTransport labelled with the given name.
// suggestion is the transport value the caller should try instead.
func NewNoopTransport(name, suggestion string) *NoopTransport {
	return &NoopTransport{transportName: name, suggestedFallback: suggestion}
}

// Name returns the transport name (e.g. "unofficial").
func (t *NoopTransport) Name() string {
	if t == nil {
		return ""
	}
	return t.transportName
}

func (t *NoopTransport) unavailable(feature string) error {
	return &ErrFeatureUnavailable{
		Feature:            feature,
		Reason:             "transport " + t.transportName + " is not implemented",
		SuggestedTransport: t.suggestedFallback,
	}
}

// ProfileMe returns ErrFeatureUnavailable.
func (t *NoopTransport) ProfileMe(_ context.Context) (*output.ProfileData, error) {
	return nil, t.unavailable("profile me")
}

// CreatePost returns ErrFeatureUnavailable.
func (t *NoopTransport) CreatePost(_ context.Context, _ CreatePostRequest) (*output.PostSummary, error) {
	return nil, t.unavailable("post create")
}

// ListPosts returns ErrFeatureUnavailable.
func (t *NoopTransport) ListPosts(_ context.Context, _ string, _, _ int) (*output.PostListData, error) {
	return nil, t.unavailable("post list")
}

// GetPost returns ErrFeatureUnavailable.
func (t *NoopTransport) GetPost(_ context.Context, _ string) (*output.PostGetData, error) {
	return nil, t.unavailable("post get")
}

// DeletePost returns ErrFeatureUnavailable.
func (t *NoopTransport) DeletePost(_ context.Context, _ string) (*output.PostDeleteData, error) {
	return nil, t.unavailable("post delete")
}

// AddComment returns ErrFeatureUnavailable.
func (t *NoopTransport) AddComment(_ context.Context, _, _ string) (*output.CommentData, error) {
	return nil, t.unavailable("comment add")
}

// ListComments returns ErrFeatureUnavailable.
func (t *NoopTransport) ListComments(_ context.Context, _ string, _, _ int) (*output.CommentListData, error) {
	return nil, t.unavailable("comment list")
}

// AddReaction returns ErrFeatureUnavailable.
func (t *NoopTransport) AddReaction(_ context.Context, _ string, _ output.ReactionType) (*output.ReactionData, error) {
	return nil, t.unavailable("react add")
}

// ListReactions returns ErrFeatureUnavailable.
func (t *NoopTransport) ListReactions(_ context.Context, _ string) (*output.ReactionListData, error) {
	return nil, t.unavailable("react list")
}

// SearchPeople returns ErrFeatureUnavailable.
func (t *NoopTransport) SearchPeople(_ context.Context, _ SearchPeopleRequest) (*output.SearchPeopleData, error) {
	return nil, t.unavailable("search people")
}

// SocialMetadata returns ErrFeatureUnavailable.
func (t *NoopTransport) SocialMetadata(_ context.Context, _ []string) (*output.SocialMetadataData, error) {
	return nil, t.unavailable("social metadata")
}
