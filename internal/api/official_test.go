package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
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
		w.Header().Set("X-RateLimit-Remaining", "42")
		w.Header().Set("X-RateLimit-Reset", "1770000000")
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

	profile, err := o.ProfileMe(t.Context())
	if err != nil {
		t.Fatalf("profile me: %v", err)
	}
	if profile.Sub != "urn:li:person:abc123" || profile.ProfileID != "abc123" {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Locale.Country != "MY" || profile.Locale.Language != "en" {
		t.Fatalf("locale = %+v", profile.Locale)
	}
	if o.Name() != "official" {
		t.Fatalf("name = %q", o.Name())
	}
	rate := o.LastRateLimit()
	if rate == nil || rate.Remaining == nil || *rate.Remaining != 42 {
		t.Fatalf("rate limit = %+v", rate)
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

	post, err := o.CreatePost(t.Context(), CreatePostRequest{
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

	list, err := o.ListPosts(t.Context(), "", 5, 0)
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

	data, err := o.DeletePost(t.Context(), "urn:li:share:9001")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !data.Deleted || data.ID != "urn:li:share:9001" {
		t.Fatalf("data = %+v", data)
	}
}

func TestOfficialGetPost(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != "/rest/posts/urn%3Ali%3Ashare%3A9001" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "urn:li:share:9001",
			"commentary": "retrieved post",
			"author":     "urn:li:person:abc123",
			"visibility": "PUBLIC",
			"createdAt":  int64(1770000000123),
		})
	}, "urn:li:person:abc123")

	post, err := o.GetPost(t.Context(), "urn:li:share:9001")
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if post.ID != "urn:li:share:9001" || post.Text != "retrieved post" {
		t.Fatalf("post = %+v", post)
	}
	if post.PublishTime != 1770000000 {
		t.Fatalf("publish_time = %d", post.PublishTime)
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

	comment, err := o.AddComment(t.Context(), "urn:li:share:9001", "nice")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if comment.ID != "urn:li:comment:99" {
		t.Fatalf("id = %q", comment.ID)
	}
}

func TestOfficialListComments(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != "/rest/socialActions/urn%3Ali%3Ashare%3A9001/comments" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		if r.URL.Query().Get("count") != "2" || r.URL.Query().Get("start") != "3" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"elements": []map[string]any{
				{
					"id":      "urn:li:comment:1",
					"actor":   "urn:li:person:abc123",
					"message": map[string]string{"text": "first"},
					"created": int64(1770000000000),
				},
			},
		})
	}, "urn:li:person:abc123")

	comments, err := o.ListComments(t.Context(), "urn:li:share:9001", 2, 3)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if comments.PostURN != "urn:li:share:9001" || comments.Count != 2 || comments.Start != 3 {
		t.Fatalf("comments metadata = %+v", comments)
	}
	if len(comments.Items) != 1 || comments.Items[0].Text != "first" {
		t.Fatalf("comments = %+v", comments.Items)
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

	data, err := o.AddReaction(t.Context(), "urn:li:share:9001", output.ReactionPraise)
	if err != nil {
		t.Fatalf("reaction: %v", err)
	}
	if data.Type != output.ReactionPraise {
		t.Fatalf("type = %q", data.Type)
	}
}

func TestOfficialListReactions(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if !strings.Contains(r.URL.RequestURI(), "/rest/reactions/(entity:urn%3Ali%3Ashare%3A9001)") {
			t.Fatalf("request URI = %q", r.URL.RequestURI())
		}
		if r.URL.Query().Get("q") != "entity" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"elements": []map[string]any{
				{
					"actor":        "urn:li:person:abc123",
					"reactionType": "LIKE",
					"created":      int64(1770000000000),
				},
			},
		})
	}, "urn:li:person:abc123")

	reactions, err := o.ListReactions(t.Context(), "urn:li:share:9001")
	if err != nil {
		t.Fatalf("list reactions: %v", err)
	}
	if reactions.Count != 1 || len(reactions.Items) != 1 {
		t.Fatalf("reactions = %+v", reactions)
	}
	if reactions.Items[0].Type != output.ReactionLike {
		t.Fatalf("reaction type = %q", reactions.Items[0].Type)
	}
}

func TestOfficialSearchPeopleReturnsUnavailable(t *testing.T) {
	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("search people must not hit the network on official transport")
	}, "urn:li:person:abc123")

	_, err := o.SearchPeople(t.Context(), SearchPeopleRequest{Keywords: "engineer"})
	if !errors.Is(err, &ErrFeatureUnavailable{}) {
		t.Fatalf("expected feature unavailable, got %v", err)
	}
}

func TestOfficialInitializeImageUpload(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/images" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "action=initializeUpload" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": map[string]any{
				"uploadUrl": "https://upload.example.com/signed/abc",
				"image":     "urn:li:image:99999",
			},
		})
	}, "urn:li:person:abc123")

	uploadURL, imageURN, err := o.InitializeImageUpload(t.Context(), "urn:li:person:abc123")
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if uploadURL != "https://upload.example.com/signed/abc" {
		t.Fatalf("uploadURL = %q", uploadURL)
	}
	if imageURN != "urn:li:image:99999" {
		t.Fatalf("imageURN = %q", imageURN)
	}
}

func TestOfficialInitializeImageUploadRejectsMissingFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "missing upload URL", body: `{"value":{"image":"urn:li:image:99999"}}`, want: "missing uploadUrl"},
		{name: "missing image URN", body: `{"value":{"uploadUrl":"https://upload.example.com/signed/abc"}}`, want: "missing image urn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}, "urn:li:person:abc123")

			_, _, err := o.InitializeImageUpload(t.Context(), "urn:li:person:abc123")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOfficialUploadImageBinary(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte

	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		// Signed URL must NOT receive Authorization header.
		if r.Header.Get("Authorization") != "" {
			t.Errorf("upload endpoint received unexpected Authorization header")
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer uploadServer.Close()

	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {}, "urn:li:person:abc123")

	// Write a small temp image file.
	tmpFile := t.TempDir() + "/test.jpg"
	if err := os.WriteFile(tmpFile, []byte("fake-image-bytes"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if err := o.UploadImageBinary(t.Context(), uploadServer.URL+"/upload", tmpFile); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotContentType != "application/octet-stream" {
		t.Fatalf("content-type = %q", gotContentType)
	}
	if string(gotBody) != "fake-image-bytes" {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestOfficialUploadImageBinaryRejectsClientError(t *testing.T) {
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer uploadServer.Close()

	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {}, "urn:li:person:abc123")
	tmpFile := t.TempDir() + "/test.jpg"
	if err := os.WriteFile(tmpFile, []byte("fake-image-bytes"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := o.UploadImageBinary(t.Context(), uploadServer.URL+"/upload", tmpFile)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 400") {
		t.Fatalf("upload error = %v, want 400", err)
	}
}

func TestOfficialUploadImageBinaryRejectsEmptyFileAndBadURL(t *testing.T) {
	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {}, "urn:li:person:abc123")
	emptyFile := t.TempDir() + "/empty.jpg"
	if err := os.WriteFile(emptyFile, nil, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if err := o.UploadImageBinary(t.Context(), "https://upload.example.com/signed", emptyFile); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty upload error = %v", err)
	}

	tmpFile := t.TempDir() + "/test.jpg"
	if err := os.WriteFile(tmpFile, []byte("fake-image-bytes"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := o.UploadImageBinary(t.Context(), "http://[::1", tmpFile); err == nil || !strings.Contains(err.Error(), "build upload request") {
		t.Fatalf("bad URL error = %v", err)
	}
}

func TestOfficialUploadImageBinaryExhaustsRetries(t *testing.T) {
	var attempts int
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer uploadServer.Close()

	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {}, "urn:li:person:abc123")
	tmpFile := t.TempDir() + "/test.jpg"
	if err := os.WriteFile(tmpFile, []byte("fake-image-bytes"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := o.UploadImageBinary(t.Context(), uploadServer.URL+"/upload", tmpFile)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 500") {
		t.Fatalf("upload error = %v, want retry exhaustion 500", err)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
}

func TestOfficialUploadImageBinaryRejectsOversized(t *testing.T) {
	o, _ := newTestOfficial(t, func(_ http.ResponseWriter, _ *http.Request) {}, "urn:li:person:abc123")

	// Write a file > 10MB.
	tmpFile := t.TempDir() + "/big.jpg"
	big := make([]byte, maxImageBytes+1)
	if err := os.WriteFile(tmpFile, big, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := o.UploadImageBinary(t.Context(), "https://upload.example.com/signed", tmpFile)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("error = %v", err)
	}
}

func TestUploadRetryWaitStopsOnCancellationAndExhaustion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if uploadRetryWait(ctx, 1, 4) {
		t.Fatal("retry wait should stop when context is cancelled")
	}
	if uploadRetryWait(t.Context(), 4, 4) {
		t.Fatal("retry wait should stop after max attempts")
	}
}

func TestOfficialEditPost204(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != "/rest/posts/urn%3Ali%3Ashare%3A42" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		patch, _ := payload["patch"].(map[string]any)
		set, _ := patch["$set"].(map[string]any)
		if set["commentary"] != "updated text" {
			t.Fatalf("commentary = %v", set["commentary"])
		}
		w.WriteHeader(http.StatusNoContent)
	}, "urn:li:person:abc123")

	text := "updated text"
	data, err := o.EditPost(t.Context(), EditPostRequest{
		PostURN: "urn:li:share:42",
		Text:    &text,
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if data.ID != "urn:li:share:42" {
		t.Fatalf("id = %q", data.ID)
	}
	if data.Text != "updated text" {
		t.Fatalf("text = %q", data.Text)
	}
	if data.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updated_at")
	}
}

func TestOfficialEditPostJSONResponse(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "urn:li:share:42",
			"commentary": "updated from body",
			"author":     "urn:li:person:abc123",
			"visibility": "PUBLIC",
			"createdAt":  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC).UnixMilli(),
			"updatedAt":  time.Date(2026, 4, 16, 12, 30, 0, 0, time.UTC).UnixMilli(),
		})
	}, "urn:li:person:abc123")

	text := "updated text"
	data, err := o.EditPost(t.Context(), EditPostRequest{
		PostURN: "urn:li:share:42",
		Text:    &text,
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if data.Text != "updated from body" || data.AuthorURN != "urn:li:person:abc123" {
		t.Fatalf("edit data = %+v", data)
	}
	if data.UpdatedAt.Format(time.RFC3339) != "2026-04-16T12:30:00Z" {
		t.Fatalf("updated_at = %s", data.UpdatedAt)
	}
}

func TestOfficialEditPostRetriesRateLimit(t *testing.T) {
	attempts := 0
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"status":429,"code":"TOO_MANY_REQUESTS","message":"slow down"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}, "urn:li:person:abc123")

	text := "updated text"
	if _, err := o.EditPost(t.Context(), EditPostRequest{
		PostURN: "urn:li:share:42",
		Text:    &text,
	}); err != nil {
		t.Fatalf("edit after retry: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestOfficialEditPostBubblesDecodedError(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"status":422,"code":"VALIDATION_ERROR","message":"bad edit"}`)
	}, "urn:li:person:abc123")

	text := "updated text"
	_, err := o.EditPost(t.Context(), EditPostRequest{
		PostURN: "urn:li:share:42",
		Text:    &text,
	})
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected api error, got %T: %v", err, err)
	}
	if !apiErr.IsValidation() || apiErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("api error = %+v, want validation", apiErr)
	}
}

func TestOfficialResharePost(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		reshare, _ := payload["reshareContext"].(map[string]any)
		if reshare["parent"] != "urn:li:share:1" {
			t.Fatalf("reshare parent = %v", reshare["parent"])
		}
		if payload["commentary"] != "worth sharing" {
			t.Fatalf("commentary = %v", payload["commentary"])
		}
		w.Header().Set("x-restli-id", "urn:li:share:9999")
		w.WriteHeader(http.StatusCreated)
	}, "urn:li:person:abc123")

	summary, err := o.ResharePost(t.Context(), ResharePostRequest{
		ParentURN:  "urn:li:share:1",
		Commentary: "worth sharing",
		Visibility: output.VisibilityPublic,
	})
	if err != nil {
		t.Fatalf("reshare: %v", err)
	}
	if summary.ID != "urn:li:share:9999" {
		t.Fatalf("id = %q", summary.ID)
	}
	if summary.Text != "worth sharing" {
		t.Fatalf("text = %q", summary.Text)
	}
}

func TestOfficialResharePostUsesBodyIDAndRejectsMissingID(t *testing.T) {
	t.Run("body id", func(t *testing.T) {
		o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "urn:li:share:body"})
		}, "urn:li:person:abc123")

		summary, err := o.ResharePost(t.Context(), ResharePostRequest{ParentURN: "urn:li:share:1"})
		if err != nil {
			t.Fatalf("reshare: %v", err)
		}
		if summary.ID != "urn:li:share:body" {
			t.Fatalf("id = %q, want body id", summary.ID)
		}
	})
	t.Run("missing id", func(t *testing.T) {
		o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		}, "urn:li:person:abc123")

		_, err := o.ResharePost(t.Context(), ResharePostRequest{ParentURN: "urn:li:share:1"})
		if err == nil || !strings.Contains(err.Error(), "missing post urn") {
			t.Fatalf("error = %v, want missing post urn", err)
		}
	})
}

func TestOfficialResharePostBubblesError(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"status":422,"code":"VALIDATION_ERROR","message":"bad reshare"}`)
	}, "urn:li:person:abc123")

	_, err := o.ResharePost(t.Context(), ResharePostRequest{ParentURN: "urn:li:share:1"})
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected api error, got %T: %v", err, err)
	}
	if !apiErr.IsValidation() || apiErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("api error = %+v, want validation", apiErr)
	}
}

func TestOfficialResharePostVersionGate(t *testing.T) {
	// Build a client with version 202201 < 202209 — reshare should be blocked.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("reshare must not hit the network when version is too old")
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:      server.URL,
		APIVersion:   "202201",
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
	o := NewOfficial(OfficialConfig{
		Client:    client,
		AuthorURN: "urn:li:person:abc123",
		Now:       func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) },
	})

	_, reshareErr := o.ResharePost(t.Context(), ResharePostRequest{
		ParentURN:  "urn:li:share:1",
		Commentary: "test",
		Visibility: output.VisibilityPublic,
	})
	if reshareErr == nil {
		t.Fatal("expected version gate error")
	}
	fe, ok := AsFeatureUnavailable(reshareErr)
	if !ok {
		t.Fatalf("expected ErrFeatureUnavailable, got %T: %v", reshareErr, reshareErr)
	}
	if !strings.Contains(fe.Reason, "202209") {
		t.Fatalf("reason = %q", fe.Reason)
	}
}

func TestOfficialListOrganizationsHydratesNames(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/organizationAcls":
			if r.URL.RawQuery != "q=roleAssignee&role=ADMINISTRATOR&state=APPROVED" {
				t.Fatalf("query = %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"elements": []map[string]any{
					{
						"organization": "urn:li:organization:111",
						"role":         "ADMINISTRATOR",
						"state":        "APPROVED",
					},
					{
						"organization": "urn:li:organization:not-a-number",
						"role":         "ADMINISTRATOR",
						"state":        "APPROVED",
					},
					{
						"organization": "urn:li:organization:222",
						"role":         "ADMINISTRATOR",
						"state":        "APPROVED",
					},
				},
			})
		case "/rest/organizations/111":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"localizedName": "Acme Corp",
				"vanityName":    "acme",
				"logoV2":        map[string]string{"original": "https://example.com/acme.png"},
			})
		case "/rest/organizations/222":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"localizedName": "Beta Ltd",
				"vanityName":    "beta",
			})
		default:
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
	}, "urn:li:person:abc123")

	orgs, err := o.ListOrganizations(t.Context())
	if err != nil {
		t.Fatalf("list organizations: %v", err)
	}
	if orgs.Count != 3 || len(orgs.Items) != 3 {
		t.Fatalf("orgs = %+v", orgs)
	}
	byURN := make(map[string]output.OrgListItem, len(orgs.Items))
	for _, item := range orgs.Items {
		byURN[item.URN] = item
	}
	if byURN["urn:li:organization:111"].Name != "Acme Corp" {
		t.Fatalf("org 111 = %+v", byURN["urn:li:organization:111"])
	}
	if byURN["urn:li:organization:111"].LogoURL != "https://example.com/acme.png" {
		t.Fatalf("org 111 logo = %q", byURN["urn:li:organization:111"].LogoURL)
	}
	if byURN["urn:li:organization:not-a-number"].Name != "" {
		t.Fatalf("invalid org should not hydrate: %+v", byURN["urn:li:organization:not-a-number"])
	}
}

// TestListOrganizationsContextCancelDoesNotLeakGoroutines verifies the
// hydration fan-out cleans up cleanly when the caller cancels mid-flight.
// Finding H12.
func TestListOrganizationsContextCancelDoesNotLeakGoroutines(t *testing.T) {
	block := make(chan struct{})
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rest/organizations/") {
			// Block until ctx cancellation propagates through the
			// http client and the request returns.
			select {
			case <-block:
			case <-r.Context().Done():
			}
			http.Error(w, "cancelled", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/rest/organizationAcls" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"elements": []map[string]any{
					{"organization": "urn:li:organization:1", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:2", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:3", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:4", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:5", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:6", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:7", "role": "ADMINISTRATOR", "state": "APPROVED"},
					{"organization": "urn:li:organization:8", "role": "ADMINISTRATOR", "state": "APPROVED"},
				},
			})
			return
		}
		t.Fatalf("unexpected path = %q", r.URL.Path)
	}, "urn:li:person:abc123")
	defer close(block)

	// Settle background goroutines from setup.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = o.ListOrganizations(ctx)
	}()

	// Give the fan-out time to dispatch workers and block in HTTP.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ListOrganizations did not return after cancel")
	}

	// Settle and check goroutine count.
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > baseline+2 {
		t.Fatalf("goroutine leak: baseline=%d after=%d", baseline, after)
	}
}

func TestOfficialCreatePostWithMedia(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		content, _ := payload["content"].(map[string]any)
		if content == nil {
			t.Fatal("expected content block")
		}
		media, _ := content["media"].(map[string]any)
		if media["id"] != "urn:li:image:99999" {
			t.Fatalf("media id = %v", media["id"])
		}
		w.Header().Set("x-restli-id", "urn:li:share:imgpost1")
		w.WriteHeader(http.StatusCreated)
	}, "urn:li:person:abc123")

	post, err := o.CreatePost(t.Context(), CreatePostRequest{
		Text:       "image post",
		Visibility: output.VisibilityPublic,
		MediaPayload: &MediaPayload{
			ID:  "urn:li:image:99999",
			Alt: "nice photo",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if post.ID != "urn:li:share:imgpost1" {
		t.Fatalf("id = %q", post.ID)
	}
}

func TestOfficialBubbles401(t *testing.T) {
	o, _ := newTestOfficial(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"status":401,"code":"UNAUTHORIZED","message":"token expired"}`)
	}, "urn:li:person:abc123")

	_, err := o.ProfileMe(t.Context())
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected api error, got %T", err)
	}
	if !apiErr.IsUnauthorized() {
		t.Fatalf("expected 401, got %+v", apiErr)
	}
}
