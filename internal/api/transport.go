package api

import (
	"context"

	"github.com/mudrii/golink/internal/output"
)

// Transport is the contract that both official and unofficial LinkedIn
// adapters must implement. Each method returns domain types that the CLI
// renders directly into --json response envelopes.
type Transport interface {
	Name() string

	ProfileMe(ctx context.Context) (*output.ProfileData, error)

	CreatePost(ctx context.Context, req CreatePostRequest) (*output.PostSummary, error)
	ListPosts(ctx context.Context, authorURN string, count, start int) (*output.PostListData, error)
	GetPost(ctx context.Context, postURN string) (*output.PostGetData, error)
	DeletePost(ctx context.Context, postURN string) (*output.PostDeleteData, error)

	AddComment(ctx context.Context, postURN, text string) (*output.CommentData, error)
	ListComments(ctx context.Context, postURN string, count, start int) (*output.CommentListData, error)

	AddReaction(ctx context.Context, postURN string, rtype output.ReactionType) (*output.ReactionData, error)
	ListReactions(ctx context.Context, postURN string) (*output.ReactionListData, error)

	SearchPeople(ctx context.Context, req SearchPeopleRequest) (*output.SearchPeopleData, error)
}

// CreatePostRequest captures the inputs needed to publish a post via the
// LinkedIn Posts API.
type CreatePostRequest struct {
	Text       string
	Visibility output.Visibility
	Media      string
}

// SearchPeopleRequest describes a people search query. Only Keywords is
// required in the current contract.
type SearchPeopleRequest struct {
	Keywords string
	Count    int
	Start    int
}
