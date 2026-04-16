package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mudrii/golink/internal/output"
)

// Official implements Transport against LinkedIn's official REST APIs. It
// resolves the authenticated member's URN once per process and reuses it for
// author defaulting in list endpoints.
type Official struct {
	client         *Client
	authorURN      string
	now            func() time.Time
	rlMu           sync.Mutex
	lastRateLimit_ *output.RateLimitInfo
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

// LastRateLimit returns the most recently observed rate-limit metadata, or nil.
// Implements RateLimitAware.
func (o *Official) LastRateLimit() *output.RateLimitInfo {
	o.rlMu.Lock()
	defer o.rlMu.Unlock()
	return o.lastRateLimit_
}

// do wraps client.Do and records the response rate-limit headers.
func (o *Official) do(ctx context.Context, method, path string, body any) (*Response, error) {
	resp, err := o.client.Do(ctx, method, path, body)
	if err == nil && resp.RateLimit != nil {
		o.rlMu.Lock()
		o.lastRateLimit_ = resp.RateLimit
		o.rlMu.Unlock()
	}
	return resp, err
}

// ProfileMe fetches the authenticated member profile via OIDC userinfo.
func (o *Official) ProfileMe(ctx context.Context) (*output.ProfileData, error) {
	resp, err := o.do(ctx, "GET", "/v2/userinfo", nil)
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

	if req.MediaPayload != nil {
		media := map[string]any{"id": req.MediaPayload.ID}
		if req.MediaPayload.Title != "" {
			media["title"] = req.MediaPayload.Title
		}
		if req.MediaPayload.Alt != "" {
			media["altText"] = req.MediaPayload.Alt
		}
		payload["content"] = map[string]any{"media": media}
	}

	resp, err := o.do(ctx, "POST", "/rest/posts", payload)
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
	resp, err := o.do(ctx, "GET", path, nil)
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
	resp, err := o.do(ctx, "GET", "/rest/posts/"+encoded, nil)
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
	_, err = o.do(ctx, "DELETE", "/rest/posts/"+encoded, nil)
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
	resp, err := o.do(ctx, "POST", "/rest/socialActions/"+encoded+"/comments", payload)
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
	resp, err := o.do(ctx, "GET", path, nil)
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
	_, err = o.do(ctx, "POST", "/rest/reactions?actor="+encodedActor, payload)
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
	resp, err := o.do(ctx, "GET", path, nil)
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

// SocialMetadata fetches engagement metadata for a batch of post URNs via
// LinkedIn's /rest/socialMetadata batch-get endpoint. Partial per-URN errors
// from LinkedIn's errors block are surfaced in the matching item rather than
// failing the whole call.
func (o *Official) SocialMetadata(ctx context.Context, urns []string) (*output.SocialMetadataData, error) {
	if len(urns) == 0 {
		return nil, fmt.Errorf("at least one urn required")
	}

	encoded := make([]string, 0, len(urns))
	for _, u := range urns {
		enc, err := EncodeURN(u)
		if err != nil {
			return nil, fmt.Errorf("encode urn %q: %w", u, err)
		}
		encoded = append(encoded, enc)
	}

	path := "/rest/socialMetadata?ids=List(" + strings.Join(encoded, ",") + ")"
	resp, err := o.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Results map[string]struct {
			CommentsSummary struct {
				TotalFirstLevelComments int `json:"totalFirstLevelComments"`
				AggregatedTotalComments int `json:"aggregatedTotalComments"`
			} `json:"commentsSummary"`
			ReactionsSummary struct {
				ReactionTypeCounts []struct {
					ReactionType string `json:"reactionType"`
					Count        int    `json:"count"`
				} `json:"reactionTypeCounts"`
				AggregatedTotalReactions int `json:"aggregatedTotalReactions"`
			} `json:"reactionsSummary"`
			CommentsState string `json:"commentsState"`
		} `json:"results"`
		Errors map[string]struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode social metadata: %w", err)
	}

	items := make([]output.SocialMetadataItem, 0, len(urns))
	for _, urn := range urns {
		item := output.SocialMetadataItem{PostURN: urn}

		if apiErr, hasErr := raw.Errors[urn]; hasErr {
			item.Error = fmt.Sprintf("status %d: %s", apiErr.Status, apiErr.Message)
			items = append(items, item)
			continue
		}

		if result, ok := raw.Results[urn]; ok {
			item.CommentCount = result.CommentsSummary.TotalFirstLevelComments
			item.AllCommentCount = result.CommentsSummary.AggregatedTotalComments
			item.ReactionCount = result.ReactionsSummary.AggregatedTotalReactions
			item.CommentsState = result.CommentsState

			counts := make(map[string]int, len(result.ReactionsSummary.ReactionTypeCounts))
			for _, rc := range result.ReactionsSummary.ReactionTypeCounts {
				counts[rc.ReactionType] = rc.Count
				if rc.ReactionType == string(output.ReactionLike) {
					item.LikeCount = rc.Count
				}
			}
			if len(counts) > 0 {
				item.ReactionCounts = counts
			}
		}

		items = append(items, item)
	}

	return &output.SocialMetadataData{
		Items: items,
		Count: len(items),
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

// maxImageBytes is the LinkedIn upload size limit for images.
const maxImageBytes = 10 * 1024 * 1024

// InitializeImageUpload calls the LinkedIn Images API to register an upload
// and obtain a signed upload URL and the image URN.
func (o *Official) InitializeImageUpload(ctx context.Context, ownerURN string) (uploadURL, imageURN string, err error) {
	owner := strings.TrimSpace(ownerURN)
	if owner == "" {
		owner = o.authorURN
	}
	if owner == "" {
		return "", "", &Error{Status: 401, Code: "UNAUTHORIZED", Message: "owner urn not resolved from session"}
	}

	payload := map[string]any{
		"initializeUploadRequest": map[string]any{
			"owner": owner,
		},
	}
	resp, err := o.do(ctx, "POST", "/rest/images?action=initializeUpload", payload)
	if err != nil {
		return "", "", fmt.Errorf("initialize image upload: %w", err)
	}

	var raw struct {
		Value struct {
			UploadURL string `json:"uploadUrl"`
			Image     string `json:"image"`
		} `json:"value"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return "", "", fmt.Errorf("decode initialize upload response: %w", err)
	}
	if raw.Value.UploadURL == "" {
		return "", "", fmt.Errorf("images api response missing uploadUrl")
	}
	if raw.Value.Image == "" {
		return "", "", fmt.Errorf("images api response missing image urn")
	}
	return raw.Value.UploadURL, raw.Value.Image, nil
}

// UploadImageBinary PUTs the file bytes to the LinkedIn signed upload URL.
// The signed URL must NOT receive an Authorization header — LinkedIn signs
// the URL itself and rejects requests that also carry a Bearer token.
func (o *Official) UploadImageBinary(ctx context.Context, uploadURL, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read image file: %w", err)
	}
	if len(data) > maxImageBytes {
		return fmt.Errorf("image file exceeds 10MB limit (%d bytes)", len(data))
	}
	if len(data) == 0 {
		return fmt.Errorf("image file is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	httpClient := o.client.retryable.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload image binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("upload image binary: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// EditPost PATCHes an existing post's commentary and/or visibility.
// LinkedIn returns 204 No Content on success; in that case a minimal
// PostEditData is constructed from the request rather than making a GET.
func (o *Official) EditPost(ctx context.Context, req EditPostRequest) (*output.PostEditData, error) {
	postURN := strings.TrimSpace(req.PostURN)
	if postURN == "" {
		return nil, fmt.Errorf("post urn must not be empty")
	}
	if req.Text == nil && req.Visibility == nil {
		return nil, fmt.Errorf("at least one of text or visibility must be provided")
	}

	encoded, err := EncodeURN(postURN)
	if err != nil {
		return nil, fmt.Errorf("encode post urn: %w", err)
	}

	set := map[string]any{}
	if req.Text != nil {
		set["commentary"] = *req.Text
	}
	if req.Visibility != nil {
		set["visibility"] = string(*req.Visibility)
	}

	body := map[string]any{
		"patch": map[string]any{"$set": set},
	}

	resp, err := o.do(ctx, http.MethodPatch, "/rest/posts/"+encoded, body)
	if err != nil {
		return nil, fmt.Errorf("edit post: %w", err)
	}

	now := o.now().UTC()

	// LinkedIn returns 204 on success — build result from the request inputs.
	if resp.Status == http.StatusNoContent || len(resp.Body) == 0 {
		text := ""
		if req.Text != nil {
			text = *req.Text
		}
		visibility := output.VisibilityPublic
		if req.Visibility != nil {
			visibility = *req.Visibility
		}
		return &output.PostEditData{
			PostSummary: output.PostSummary{
				ID:         postURN,
				CreatedAt:  now,
				Text:       text,
				Visibility: visibility,
				URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/%s", postURN),
				AuthorURN:  o.authorURN,
			},
			UpdatedAt: now,
		}, nil
	}

	var raw struct {
		ID         string `json:"id"`
		Commentary string `json:"commentary"`
		Author     string `json:"author"`
		Visibility string `json:"visibility"`
		CreatedAt  int64  `json:"createdAt"`
	}
	if err := resp.UnmarshalJSON(&raw); err != nil {
		return nil, fmt.Errorf("decode edit post response: %w", err)
	}

	created := now
	if raw.CreatedAt > 0 {
		created = time.UnixMilli(raw.CreatedAt).UTC()
	}
	id := raw.ID
	if id == "" {
		id = postURN
	}
	return &output.PostEditData{
		PostSummary: output.PostSummary{
			ID:         id,
			CreatedAt:  created,
			Text:       raw.Commentary,
			Visibility: output.Visibility(raw.Visibility),
			URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/%s", id),
			AuthorURN:  raw.Author,
		},
		UpdatedAt: now,
	}, nil
}

// compareVersionYYYYMM compares two YYYYMM version strings. Returns negative
// if a < b, zero if equal, positive if a > b. Non-numeric strings sort low.
func compareVersionYYYYMM(a, b string) int {
	ai, _ := parseInt(a)
	bi, _ := parseInt(b)
	return ai - bi
}

func parseInt(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// ResharePost creates a new post that reshares an existing share URN.
// Requires Linkedin-Version >= 202209.
func (o *Official) ResharePost(ctx context.Context, req ResharePostRequest) (*output.PostSummary, error) {
	parentURN := strings.TrimSpace(req.ParentURN)
	if parentURN == "" {
		return nil, fmt.Errorf("parent urn must not be empty")
	}
	author := strings.TrimSpace(o.authorURN)
	if author == "" {
		return nil, &Error{Status: 401, Code: "UNAUTHORIZED", Message: "author urn not resolved from session"}
	}

	// Gate on Linkedin-Version >= 202209.
	effective := strings.TrimSpace(o.client.apiVers)
	if effective != "" && compareVersionYYYYMM(effective, "202209") < 0 {
		return nil, &ErrFeatureUnavailable{
			Feature: "post reshare",
			Reason:  fmt.Sprintf("reshare requires Linkedin-Version >= 202209 (configured: %s)", effective),
		}
	}

	visibility := req.Visibility
	if visibility == "" {
		visibility = output.VisibilityPublic
	}

	payload := map[string]any{
		"author":                    author,
		"commentary":                req.Commentary,
		"visibility":                string(visibility),
		"lifecycleState":            "PUBLISHED",
		"isReshareDisabledByAuthor": false,
		"distribution": map[string]any{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []any{},
			"thirdPartyDistributionChannels": []any{},
		},
		"reshareContext": map[string]any{
			"parent": parentURN,
		},
	}

	resp, err := o.do(ctx, "POST", "/rest/posts", payload)
	if err != nil {
		return nil, fmt.Errorf("reshare post: %w", err)
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
		Text:       req.Commentary,
		Visibility: visibility,
		URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/%s", id),
		AuthorURN:  author,
	}, nil
}
