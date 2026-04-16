package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/output"
)

// Official implements Transport against LinkedIn's official REST APIs. It
// resolves the authenticated member's URN once per process and reuses it for
// author defaulting in list endpoints.
type Official struct {
	client    *Client
	authorURN string
	now       func() time.Time
}

// OfficialConfig provides the wiring required to build an Official transport.
type OfficialConfig struct {
	Client    *Client
	AuthorURN string
	Now       func() time.Time
}

// NewOfficial constructs an Official transport.
func NewOfficial(cfg OfficialConfig) *Official {
	if cfg.Client == nil {
		return nil
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Official{
		client:    cfg.Client,
		authorURN: strings.TrimSpace(cfg.AuthorURN),
		now:       now,
	}
}

// Name returns "official".
func (*Official) Name() string { return "official" }

// ProfileMe fetches the authenticated member profile via OIDC userinfo.
func (o *Official) ProfileMe(ctx context.Context) (*output.ProfileData, error) {
	resp, err := o.client.Do(ctx, "GET", "/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Sub     string `json:"sub"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture string `json:"picture"`
		Locale  struct {
			Country  string `json:"country"`
			Language string `json:"language"`
		} `json:"locale"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if strings.TrimSpace(raw.Sub) == "" {
		return nil, fmt.Errorf("userinfo response missing sub")
	}

	return &output.ProfileData{
		Sub:       raw.Sub,
		Name:      raw.Name,
		Email:     raw.Email,
		Picture:   raw.Picture,
		ProfileID: strings.TrimPrefix(raw.Sub, "urn:li:person:"),
		Locale: output.Locale{
			Country:  raw.Locale.Country,
			Language: raw.Locale.Language,
		},
	}, nil
}

// CreatePost publishes a post via the Posts API.
func (o *Official) CreatePost(ctx context.Context, req CreatePostRequest) (*output.PostSummary, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("post text must not be empty")
	}
	author := strings.TrimSpace(o.authorURN)
	if author == "" {
		return nil, &Error{Status: 401, Code: "UNAUTHORIZED", Message: "author urn not resolved from session"}
	}

	visibility := req.Visibility
	if visibility == "" {
		visibility = output.VisibilityPublic
	}

	payload := map[string]any{
		"author":                    author,
		"commentary":                req.Text,
		"visibility":                string(visibility),
		"lifecycleState":            "PUBLISHED",
		"isReshareDisabledByAuthor": false,
		"distribution": map[string]any{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []any{},
			"thirdPartyDistributionChannels": []any{},
		},
	}

	resp, err := o.client.Do(ctx, "POST", "/rest/posts", payload)
	if err != nil {
		return nil, err
	}

	id := resp.Header.Get("x-restli-id")
	if id == "" {
		id = resp.Header.Get("X-RestLi-Id")
	}
	if id == "" {
		var body struct {
			ID string `json:"id"`
		}
		_ = resp.UnmarshalJSON(&body)
		id = body.ID
	}
	if id == "" {
		return nil, fmt.Errorf("posts api response missing post urn")
	}

	return &output.PostSummary{
		ID:         id,
		CreatedAt:  o.now().UTC(),
		Text:       req.Text,
		Visibility: visibility,
		URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/%s", id),
		AuthorURN:  author,
	}, nil
}

// ListPosts returns posts authored by authorURN (defaults to the session's
// member URN when empty).
func (o *Official) ListPosts(ctx context.Context, authorURN string, count, start int) (*output.PostListData, error) {
	author := strings.TrimSpace(authorURN)
	if author == "" {
		author = o.authorURN
	}
	if author == "" {
		return nil, &Error{Status: 401, Code: "UNAUTHORIZED", Message: "author urn not provided"}
	}
	if count <= 0 {
		count = 10
	}
	if start < 0 {
		start = 0
	}

	encoded, err := EncodeURN(author)
	if err != nil {
		return nil, fmt.Errorf("encode author urn: %w", err)
	}

	path := fmt.Sprintf("/rest/posts?q=author&author=%s&count=%d&start=%d", encoded, count, start)
	resp, err := o.client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Elements []struct {
			ID         string `json:"id"`
			Commentary string `json:"commentary"`
			Author     string `json:"author"`
			Visibility string `json:"visibility"`
			CreatedAt  int64  `json:"createdAt"`
		} `json:"elements"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode posts list: %w", err)
	}

	items := make([]output.PostListItem, 0, len(raw.Elements))
	for _, el := range raw.Elements {
		items = append(items, output.PostListItem{
			PostSummary: output.PostSummary{
				ID:         el.ID,
				CreatedAt:  time.UnixMilli(el.CreatedAt).UTC(),
				Text:       el.Commentary,
				Visibility: output.Visibility(el.Visibility),
				URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/%s", el.ID),
				AuthorURN:  el.Author,
			},
		})
	}

	return &output.PostListData{
		OwnerURN: author,
		Count:    count,
		Start:    start,
		Items:    items,
	}, nil
}

// GetPost retrieves a single post. LinkedIn's Posts API retrieval is
// entitlement-gated; callers should be prepared for ErrFeatureUnavailable
// surfaced through a 403 response.
func (o *Official) GetPost(ctx context.Context, postURN string) (*output.PostGetData, error) {
	encoded, err := EncodeURN(postURN)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.Do(ctx, "GET", "/rest/posts/"+encoded, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ID         string `json:"id"`
		Commentary string `json:"commentary"`
		Author     string `json:"author"`
		Visibility string `json:"visibility"`
		CreatedAt  int64  `json:"createdAt"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode post get: %w", err)
	}

	return &output.PostGetData{
		PostSummary: output.PostSummary{
			ID:         raw.ID,
			CreatedAt:  time.UnixMilli(raw.CreatedAt).UTC(),
			Text:       raw.Commentary,
			Visibility: output.Visibility(raw.Visibility),
			URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/%s", raw.ID),
			AuthorURN:  raw.Author,
		},
		PublishTime: raw.CreatedAt / 1000,
	}, nil
}

// DeletePost deletes a post via the Posts API.
func (o *Official) DeletePost(ctx context.Context, postURN string) (*output.PostDeleteData, error) {
	encoded, err := EncodeURN(postURN)
	if err != nil {
		return nil, err
	}
	_, err = o.client.Do(ctx, "DELETE", "/rest/posts/"+encoded, nil)
	if err != nil {
		return nil, err
	}
	return &output.PostDeleteData{ID: strings.TrimSpace(postURN), Deleted: true}, nil
}

// AddComment posts a comment against the given share/ugcPost URN. LinkedIn's
// Community Management API requires the path to contain the share URN and the
// body to reference the corresponding activity URN.
func (o *Official) AddComment(ctx context.Context, postURN, text string) (*output.CommentData, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("comment text must not be empty")
	}
	actor := strings.TrimSpace(o.authorURN)
	if actor == "" {
		return nil, &Error{Status: 401, Code: "UNAUTHORIZED", Message: "actor urn not resolved from session"}
	}
	encoded, err := EncodeURN(postURN)
	if err != nil {
		return nil, err
	}
	activityURN, err := ActivityURNFromPost(postURN)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"actor":   actor,
		"object":  activityURN,
		"message": map[string]any{"text": text},
	}
	resp, err := o.client.Do(ctx, "POST", "/rest/socialActions/"+encoded+"/comments", payload)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created"`
	}
	_ = resp.UnmarshalJSON(&raw)
	id := raw.ID
	if id == "" {
		id = resp.Header.Get("x-restli-id")
	}
	if id == "" {
		id = resp.Header.Get("X-RestLi-Id")
	}

	created := o.now().UTC()
	if raw.CreatedAt > 0 {
		created = time.UnixMilli(raw.CreatedAt).UTC()
	}

	return &output.CommentData{
		ID:        id,
		PostURN:   postURN,
		Author:    actor,
		Text:      text,
		CreatedAt: created,
	}, nil
}

// ListComments returns comments for the given share/ugcPost URN.
func (o *Official) ListComments(ctx context.Context, postURN string, count, start int) (*output.CommentListData, error) {
	if count <= 0 {
		count = 10
	}
	if start < 0 {
		start = 0
	}
	encoded, err := EncodeURN(postURN)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/rest/socialActions/%s/comments?count=%d&start=%d", encoded, count, start)
	resp, err := o.client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Elements []struct {
			ID      string `json:"id"`
			Actor   string `json:"actor"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Created int64 `json:"created"`
		} `json:"elements"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode comment list: %w", err)
	}

	items := make([]output.CommentData, 0, len(raw.Elements))
	for _, el := range raw.Elements {
		items = append(items, output.CommentData{
			ID:        el.ID,
			PostURN:   postURN,
			Author:    el.Actor,
			Text:      el.Message.Text,
			CreatedAt: time.UnixMilli(el.Created).UTC(),
		})
	}

	return &output.CommentListData{
		PostURN: postURN,
		Items:   items,
		Count:   count,
		Start:   start,
	}, nil
}

// AddReaction registers a reaction on the given share/ugcPost URN.
func (o *Official) AddReaction(ctx context.Context, postURN string, rtype output.ReactionType) (*output.ReactionData, error) {
	actor := strings.TrimSpace(o.authorURN)
	if actor == "" {
		return nil, &Error{Status: 401, Code: "UNAUTHORIZED", Message: "actor urn not resolved from session"}
	}
	if postURN == "" {
		return nil, fmt.Errorf("post urn must not be empty")
	}
	if rtype == "" {
		rtype = output.ReactionLike
	}

	encodedActor, err := EncodeURN(actor)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"root":         postURN,
		"reactionType": string(rtype),
	}
	_, err = o.client.Do(ctx, "POST", "/rest/reactions?actor="+encodedActor, payload)
	if err != nil {
		return nil, err
	}

	return &output.ReactionData{
		PostURN: postURN,
		Actor:   actor,
		Type:    rtype,
		At:      o.now().UTC(),
	}, nil
}

// ListReactions returns reactions for the given entity URN.
func (o *Official) ListReactions(ctx context.Context, postURN string) (*output.ReactionListData, error) {
	encoded, err := EncodeURN(postURN)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/rest/reactions/(entity:%s)?q=entity&sort=(value:REVERSE_CHRONOLOGICAL)", encoded)
	resp, err := o.client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Elements []struct {
			Actor        string `json:"actor"`
			ReactionType string `json:"reactionType"`
			Created      int64  `json:"created"`
		} `json:"elements"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode reactions: %w", err)
	}

	items := make([]output.ReactionData, 0, len(raw.Elements))
	for _, el := range raw.Elements {
		items = append(items, output.ReactionData{
			PostURN: postURN,
			Actor:   el.Actor,
			Type:    output.ReactionType(el.ReactionType),
			At:      time.UnixMilli(el.Created).UTC(),
		})
	}

	return &output.ReactionListData{
		PostURN: postURN,
		Items:   items,
		Count:   len(items),
	}, nil
}

// SearchPeople is not part of LinkedIn's open self-serve permissions, so the
// official adapter always returns ErrFeatureUnavailable.
func (o *Official) SearchPeople(_ context.Context, _ SearchPeopleRequest) (*output.SearchPeopleData, error) {
	return nil, &ErrFeatureUnavailable{
		Feature:            "search people",
		Reason:             "not available through open self-serve LinkedIn consumer/community permissions",
		SuggestedTransport: "unofficial",
	}
}
