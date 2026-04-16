package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/output"
)

func newTestOfficial(t *testing.T, handler http.HandlerFunc, author string) (*Official, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(ClientConfig{
		BaseURL:      server.URL,
		APIVersion:   "202604",
		RetryMax:     1,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: time.Millisecond,
		Token: func(_ context.Context) (string, error) {
			return "token", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return NewOfficial(OfficialConfig{
		Client:    client,
		AuthorURN: author,
		Now:       func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) },
	}), server
}

func TestOfficialProfileMe(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/userinfo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":     "urn:li:person:abc123",
			"name":    "Ion Mudreac",
			"email":   "ion@example.com",
			"picture": "https://example.com/pic.jpg",
			"locale": map[string]string{
				"country":  "MY",
				"language": "en",
			},
		})
	}, "urn:li:person:abc123")

	profile, err := o.ProfileMe(context.Background())
	if err != nil {
		t.Fatalf("profile me: %v", err)
	}
	if profile.Sub != "urn:li:person:abc123" || profile.ProfileID != "abc123" {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Locale.Country != "MY" || profile.Locale.Language != "en" {
		t.Fatalf("locale = %+v", profile.Locale)
	}
}

func TestOfficialCreatePost(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/posts" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["author"] != "urn:li:person:abc123" {
			t.Fatalf("author = %v", payload["author"])
		}
		if payload["commentary"] != "Hello world" {
			t.Fatalf("commentary = %v", payload["commentary"])
		}
		if payload["visibility"] != "PUBLIC" {
			t.Fatalf("visibility = %v", payload["visibility"])
		}
		if payload["lifecycleState"] != "PUBLISHED" {
			t.Fatalf("lifecycleState = %v", payload["lifecycleState"])
		}
		w.Header().Set("x-restli-id", "urn:li:share:9001")
		w.WriteHeader(http.StatusCreated)
	}, "urn:li:person:abc123")

	post, err := o.CreatePost(context.Background(), CreatePostRequest{
		Text:       "Hello world",
		Visibility: output.VisibilityPublic,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if post.ID != "urn:li:share:9001" {
		t.Fatalf("id = %q", post.ID)
	}
	if post.URL != "https://www.linkedin.com/feed/update/urn:li:share:9001" {
		t.Fatalf("url = %q", post.URL)
	}
	if post.AuthorURN != "urn:li:person:abc123" {
		t.Fatalf("author = %q", post.AuthorURN)
	}
}

func TestOfficialListPostsDefaultsToSessionAuthor(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/posts" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("q") != "author" {
			t.Fatalf("q = %q", q.Get("q"))
		}
		if q.Get("author") != "urn:li:person:abc123" {
			t.Fatalf("author = %q", q.Get("author"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"elements": []map[string]any{
				{
					"id":         "urn:li:share:1",
					"commentary": "first",
					"author":     "urn:li:person:abc123",
					"visibility": "PUBLIC",
					"createdAt":  int64(1766300000000),
				},
			},
		})
	}, "urn:li:person:abc123")

	list, err := o.ListPosts(context.Background(), "", 5, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.OwnerURN != "urn:li:person:abc123" || len(list.Items) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if list.Items[0].ID != "urn:li:share:1" {
		t.Fatalf("item = %+v", list.Items[0])
	}
}

func TestOfficialDeletePostEncodesPathURN(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != "/rest/posts/urn%3Ali%3Ashare%3A9001" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		w.WriteHeader(http.StatusNoContent)
	}, "urn:li:person:abc123")

	data, err := o.DeletePost(context.Background(), "urn:li:share:9001")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !data.Deleted || data.ID != "urn:li:share:9001" {
		t.Fatalf("data = %+v", data)
	}
}

func TestOfficialAddCommentUsesActivityURN(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.EscapedPath(), "/comments") {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["object"] != "urn:li:activity:9001" {
			t.Fatalf("object = %v", payload["object"])
		}
		message, ok := payload["message"].(map[string]any)
		if !ok || message["text"] != "nice" {
			t.Fatalf("message = %v", payload["message"])
		}
		w.Header().Set("x-restli-id", "urn:li:comment:99")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}, "urn:li:person:abc123")

	comment, err := o.AddComment(context.Background(), "urn:li:share:9001", "nice")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if comment.ID != "urn:li:comment:99" {
		t.Fatalf("id = %q", comment.ID)
	}
}

func TestOfficialAddReactionEncodesActor(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/reactions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "actor=urn%3Ali%3Aperson%3Aabc123" {
			t.Fatalf("raw query = %q", r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["reactionType"] != "PRAISE" {
			t.Fatalf("reactionType = %v", payload["reactionType"])
		}
		w.WriteHeader(http.StatusCreated)
	}, "urn:li:person:abc123")

	data, err := o.AddReaction(context.Background(), "urn:li:share:9001", output.ReactionPraise)
	if err != nil {
		t.Fatalf("reaction: %v", err)
	}
	if data.Type != output.ReactionPraise {
		t.Fatalf("type = %q", data.Type)
	}
}

func TestOfficialSearchPeopleReturnsUnavailable(t *testing.T) {
	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("search people must not hit the network on official transport")
	}, "urn:li:person:abc123")

	_, err := o.SearchPeople(context.Background(), SearchPeopleRequest{Keywords: "engineer"})
	if !errors.Is(err, &ErrFeatureUnavailable{}) {
		t.Fatalf("expected feature unavailable, got %v", err)
	}
}

func TestOfficialBubbles401(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"status":401,"code":"UNAUTHORIZED","message":"token expired"}`)
	}, "urn:li:person:abc123")

	_, err := o.ProfileMe(context.Background())
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected api error, got %T", err)
	}
	if !apiErr.IsUnauthorized() {
		t.Fatalf("expected 401, got %+v", apiErr)
	}
}
