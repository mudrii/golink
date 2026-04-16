package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficialSocialMetadata_BatchSuccess(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/socialMetadata" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		ids := r.URL.Query().Get("ids")
		if !strings.Contains(ids, "List(") {
			t.Fatalf("ids param missing List() wrapper: %q", ids)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"urn:li:share:1": map[string]any{
					"commentsSummary": map[string]any{
						"totalFirstLevelComments": 3,
						"aggregatedTotalComments": 5,
					},
					"reactionsSummary": map[string]any{
						"reactionTypeCounts": []map[string]any{
							{"reactionType": "LIKE", "count": 12},
							{"reactionType": "PRAISE", "count": 3},
						},
						"aggregatedTotalReactions": 15,
					},
					"commentsState": "ENABLED",
				},
			},
			"errors": map[string]any{
				"urn:li:share:2": map[string]any{
					"status":  404,
					"message": "not found",
				},
			},
		})
	}, "urn:li:person:abc123")

	data, err := o.SocialMetadata(context.Background(), []string{"urn:li:share:1", "urn:li:share:2"})
	if err != nil {
		t.Fatalf("SocialMetadata: %v", err)
	}
	if data.Count != 2 {
		t.Fatalf("count = %d, want 2", data.Count)
	}
	if len(data.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(data.Items))
	}

	// First item: success
	item1 := data.Items[0]
	if item1.PostURN != "urn:li:share:1" {
		t.Errorf("item1.PostURN = %q", item1.PostURN)
	}
	if item1.CommentCount != 3 {
		t.Errorf("item1.CommentCount = %d, want 3", item1.CommentCount)
	}
	if item1.AllCommentCount != 5 {
		t.Errorf("item1.AllCommentCount = %d, want 5", item1.AllCommentCount)
	}
	if item1.ReactionCount != 15 {
		t.Errorf("item1.ReactionCount = %d, want 15", item1.ReactionCount)
	}
	if item1.LikeCount != 12 {
		t.Errorf("item1.LikeCount = %d, want 12", item1.LikeCount)
	}
	if item1.ReactionCounts["PRAISE"] != 3 {
		t.Errorf("item1.ReactionCounts[PRAISE] = %d, want 3", item1.ReactionCounts["PRAISE"])
	}
	if item1.CommentsState != "ENABLED" {
		t.Errorf("item1.CommentsState = %q, want ENABLED", item1.CommentsState)
	}
	if item1.Error != "" {
		t.Errorf("item1.Error should be empty, got %q", item1.Error)
	}

	// Second item: error surfaced in item
	item2 := data.Items[1]
	if item2.PostURN != "urn:li:share:2" {
		t.Errorf("item2.PostURN = %q", item2.PostURN)
	}
	if item2.Error == "" {
		t.Errorf("item2.Error should be non-empty")
	}
	if item2.LikeCount != 0 || item2.ReactionCount != 0 {
		t.Errorf("item2 counts should be zero on error")
	}
}

func TestOfficialSocialMetadata_URNEncoding(t *testing.T) {
	var capturedQuery string
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{},
			"errors":  map[string]any{},
		})
	}, "")

	_, _ = o.SocialMetadata(context.Background(), []string{"urn:li:share:123"})
	// colons must be percent-encoded inside the List() wrapper
	if !strings.Contains(capturedQuery, "urn%3Ali%3Ashare%3A123") {
		t.Errorf("URN not percent-encoded in query: %q", capturedQuery)
	}
}

func TestOfficialSocialMetadata_4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":401,"message":"Unauthorized"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIVersion: "202604",
		RetryMax:   0,
		Token: func(_ context.Context) (string, error) {
			return "token", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	o := NewOfficial(OfficialConfig{Client: client})

	_, err = o.SocialMetadata(context.Background(), []string{"urn:li:share:1"})
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("expected IsUnauthorized, got status %d", apiErr.Status)
	}
}

func TestOfficialSocialMetadata_EmptyURNs(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called for empty URN list")
	}, "")

	_, err := o.SocialMetadata(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty urns")
	}
}
