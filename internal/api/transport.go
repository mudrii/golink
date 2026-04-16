package api

import (
	"context"

	"github.com/mudrii/golink/internal/output"
)

// RateLimitAware is implemented by transports that expose the most recently
// observed rate-limit headers. The batch runner uses this for in-process pacing.
type RateLimitAware interface {
	LastRateLimit() *output.RateLimitInfo
}

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

	InitializeImageUpload(ctx context.Context, ownerURN string) (uploadURL, imageURN string, err error)
	UploadImageBinary(ctx context.Context, uploadURL, filePath string) error

	EditPost(ctx context.Context, req EditPostRequest) (*output.PostEditData, error)
	ResharePost(ctx context.Context, req ResharePostRequest) (*output.PostSummary, error)

	AddComment(ctx context.Context, postURN, text string) (*output.CommentData, error)
	ListComments(ctx context.Context, postURN string, count, start int) (*output.CommentListData, error)

	AddReaction(ctx context.Context, postURN string, rtype output.ReactionType) (*output.ReactionData, error)
	ListReactions(ctx context.Context, postURN string) (*output.ReactionListData, error)

	SearchPeople(ctx context.Context, req SearchPeopleRequest) (*output.SearchPeopleData, error)

	SocialMetadata(ctx context.Context, urns []string) (*output.SocialMetadataData, error)
}

// CreatePostRequest captures the inputs needed to publish a post via the
// LinkedIn Posts API.
type CreatePostRequest struct {
	Text       string
	Visibility output.Visibility
	Media      string
	// MediaPayload carries a pre-uploaded image attachment. When non-nil,
	// CreatePost includes a content.media block in the Posts API payload.
	MediaPayload *MediaPayload
}

// MediaPayload describes an image attachment for a LinkedIn post.
type MediaPayload struct {
	// ID is the image URN returned by InitializeImageUpload.
	ID    string
	Title string
	Alt   string
}

// EditPostRequest describes a partial update to an existing post.
type EditPostRequest struct {
	PostURN    string
	Text       *string
	Visibility *output.Visibility
}

// ResharePostRequest describes a reshare of an existing post.
type ResharePostRequest struct {
	ParentURN  string
	Commentary string
	Visibility output.Visibility
}

// SearchPeopleRequest describes a people search query. Only Keywords is
// required in the current contract.
type SearchPeopleRequest struct {
	Keywords string
	Count    int
	Start    int
}
