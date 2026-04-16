package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/output"
)

// fakeTransport implements api.Transport for end-to-end command tests.
type fakeTransport struct {
	name string
}

func (f *fakeTransport) Name() string { return f.name }

func (f *fakeTransport) ProfileMe(context.Context) (*output.ProfileData, error) {
	return &output.ProfileData{Sub: "urn:li:person:abc123"}, nil
}

func (f *fakeTransport) CreatePost(_ context.Context, req api.CreatePostRequest) (*output.PostSummary, error) {
	return &output.PostSummary{
		ID:         "urn:li:share:42",
		CreatedAt:  time.Date(2026, 4, 16, 12, 0, 1, 0, time.UTC),
		Text:       req.Text,
		Visibility: req.Visibility,
		URL:        "https://www.linkedin.com/feed/update/urn:li:share:42",
		AuthorURN:  "urn:li:person:abc123",
	}, nil
}

func (f *fakeTransport) ListPosts(_ context.Context, authorURN string, count, start int) (*output.PostListData, error) {
	if authorURN == "" {
		authorURN = "urn:li:person:abc123"
	}
	return &output.PostListData{
		OwnerURN: authorURN,
		Count:    count,
		Start:    start,
		Items: []output.PostListItem{{
			PostSummary: output.PostSummary{
				ID:         "urn:li:share:1",
				CreatedAt:  time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC),
				Text:       "hello",
				Visibility: output.VisibilityPublic,
				URL:        "https://www.linkedin.com/feed/update/urn:li:share:1",
				AuthorURN:  authorURN,
			},
		}},
	}, nil
}

func (f *fakeTransport) GetPost(_ context.Context, postURN string) (*output.PostGetData, error) {
	return &output.PostGetData{
		PostSummary: output.PostSummary{
			ID:         postURN,
			CreatedAt:  time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC),
			Text:       "hello",
			Visibility: output.VisibilityPublic,
			URL:        "https://www.linkedin.com/feed/update/" + postURN,
			AuthorURN:  "urn:li:person:abc123",
		},
	}, nil
}

func (f *fakeTransport) DeletePost(_ context.Context, postURN string) (*output.PostDeleteData, error) {
	return &output.PostDeleteData{ID: postURN, Deleted: true}, nil
}

func (f *fakeTransport) AddComment(_ context.Context, postURN, text string) (*output.CommentData, error) {
	return &output.CommentData{
		ID:        "urn:li:comment:7",
		PostURN:   postURN,
		Author:    "urn:li:person:abc123",
		Text:      text,
		CreatedAt: time.Date(2026, 4, 16, 12, 0, 2, 0, time.UTC),
	}, nil
}

func (f *fakeTransport) ListComments(_ context.Context, postURN string, count, start int) (*output.CommentListData, error) {
	return &output.CommentListData{PostURN: postURN, Count: count, Start: start}, nil
}

func (f *fakeTransport) AddReaction(_ context.Context, postURN string, rtype output.ReactionType) (*output.ReactionData, error) {
	return &output.ReactionData{
		PostURN: postURN,
		Actor:   "urn:li:person:abc123",
		Type:    rtype,
		At:      time.Date(2026, 4, 16, 12, 0, 3, 0, time.UTC),
	}, nil
}

func (f *fakeTransport) ListReactions(_ context.Context, postURN string) (*output.ReactionListData, error) {
	return &output.ReactionListData{PostURN: postURN}, nil
}

func (f *fakeTransport) SearchPeople(_ context.Context, req api.SearchPeopleRequest) (*output.SearchPeopleData, error) {
	return nil, &api.ErrFeatureUnavailable{
		Feature:            "search people",
		Reason:             "not available on official transport",
		SuggestedTransport: "unofficial",
	}
}

func (f *fakeTransport) SocialMetadata(_ context.Context, urns []string) (*output.SocialMetadataData, error) {
	items := make([]output.SocialMetadataItem, 0, len(urns))
	for _, u := range urns {
		items = append(items, output.SocialMetadataItem{
			PostURN:       u,
			LikeCount:     5,
			CommentCount:  2,
			ReactionCount: 5,
			CommentsState: "ENABLED",
		})
	}
	return &output.SocialMetadataData{Items: items, Count: len(items)}, nil
}

func (f *fakeTransport) InitializeImageUpload(_ context.Context, _ string) (string, string, error) {
	return "https://upload.example.com/signed", "urn:li:image:fake123", nil
}

func (f *fakeTransport) UploadImageBinary(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeTransport) EditPost(_ context.Context, req api.EditPostRequest) (*output.PostEditData, error) {
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
			ID:         req.PostURN,
			CreatedAt:  time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC),
			Text:       text,
			Visibility: visibility,
			URL:        "https://www.linkedin.com/feed/update/" + req.PostURN,
			AuthorURN:  "urn:li:person:abc123",
		},
		UpdatedAt: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (f *fakeTransport) ResharePost(_ context.Context, req api.ResharePostRequest) (*output.PostSummary, error) {
	return &output.PostSummary{
		ID:         "urn:li:share:reshare1",
		CreatedAt:  time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Text:       req.Commentary,
		Visibility: req.Visibility,
		URL:        "https://www.linkedin.com/feed/update/urn:li:share:reshare1",
		AuthorURN:  "urn:li:person:abc123",
	}, nil
}

func (f *fakeTransport) ListOrganizations(_ context.Context) (*output.OrgListData, error) {
	return &output.OrgListData{
		Count: 2,
		Items: []output.OrgListItem{
			{URN: "urn:li:organization:111", Role: "ADMINISTRATOR", State: "APPROVED", Name: "Acme Corp"},
			{URN: "urn:li:organization:222", Role: "ADMINISTRATOR", State: "APPROVED", Name: "Beta Ltd"},
		},
	}, nil
}

func authenticatedStore(t *testing.T) auth.Store {
	t.Helper()
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "token-xyz",
		MemberURN:   "urn:li:person:abc123",
		ProfileID:   "abc123",
		Name:        "Ion Mudreac",
		Email:       "ion@example.com",
		ExpiresAt:   time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return store
}

func factoryReturning(transport api.Transport) TransportFactory {
	return func(_ context.Context, _ config.Settings, _ auth.Session, _ *slog.Logger) (api.Transport, error) {
		return transport, nil
	}
}

func TestPostCreateLive(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "Hello from test"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || payload.Data.ID != "urn:li:share:42" {
		t.Fatalf("envelope = %+v", payload)
	}
}

func TestPostListLive(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "post", "list", "--count", "5"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestCommentAddLive(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "comment", "add", "urn:li:share:42", "--text", "nice"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestCommentAddDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "comment", "add", "urn:li:share:42", "--text", "nice"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Mode != "dry_run" {
		t.Fatalf("mode = %q", payload.Mode)
	}
}

func TestReactAddLive(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "react", "add", "urn:li:share:42", "--type", "PRAISE"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestReactAddDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "react", "add", "urn:li:share:42", "--type", "LIKE"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestPostDeleteDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "post", "delete", "urn:li:share:42"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestSearchPeopleOfficialReturnsUnsupported(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "search", "people", "--keywords", "engineer"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Feature string `json:"feature"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "unsupported" || payload.Data.Feature != "search people" {
		t.Fatalf("envelope = %+v", payload)
	}
}

func TestPostEditDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "post", "edit", "urn:li:share:42", "--text", "new body"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Mode string `json:"mode"`
		Data struct {
			Mode       string `json:"mode"`
			WouldPatch struct {
				PostURN string `json:"post_urn"`
			} `json:"would_patch"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Mode != "dry_run" {
		t.Fatalf("envelope mode = %q", payload.Mode)
	}
	if payload.Data.WouldPatch.PostURN != "urn:li:share:42" {
		t.Fatalf("post_urn = %q", payload.Data.WouldPatch.PostURN)
	}
}

func TestPostEditNoFlagsFails(t *testing.T) {
	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "post", "edit", "urn:li:share:42"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 2 {
		t.Fatalf("expected exit 2, got %d stderr=%s", code, stderr)
	}
}

func TestPostEditLive(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "post", "edit", "urn:li:share:42", "--text", "updated"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || payload.Data.ID != "urn:li:share:42" {
		t.Fatalf("envelope = %+v", payload)
	}
}

func TestPostReshareDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "post", "reshare", "urn:li:share:1", "--text", "worth sharing"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Mode string `json:"mode"`
		Data struct {
			WouldReshare struct {
				ParentURN string `json:"parent_urn"`
			} `json:"would_reshare"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Mode != "dry_run" {
		t.Fatalf("mode = %q", payload.Mode)
	}
	if payload.Data.WouldReshare.ParentURN != "urn:li:share:1" {
		t.Fatalf("parent_urn = %q", payload.Data.WouldReshare.ParentURN)
	}
}

func TestPostReshareLive(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "post", "reshare", "urn:li:share:1", "--text", "great post"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || payload.Data.ID != "urn:li:share:reshare1" {
		t.Fatalf("envelope = %+v", payload)
	}
}

func TestPostCreateImageDryRun(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "post", "create", "--text", "image post", "--image", "/tmp/photo.jpg", "--image-alt", "a nice photo"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
	var payload struct {
		Data struct {
			WouldPost struct {
				WouldUpload *struct {
					Path           string `json:"path"`
					PlaceholderURN string `json:"placeholder_urn"`
					Alt            string `json:"alt"`
				} `json:"would_upload"`
			} `json:"would_post"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data.WouldPost.WouldUpload == nil {
		t.Fatal("expected would_upload in dry-run output")
	}
	if payload.Data.WouldPost.WouldUpload.Path != "/tmp/photo.jpg" {
		t.Fatalf("path = %q", payload.Data.WouldPost.WouldUpload.Path)
	}
	if payload.Data.WouldPost.WouldUpload.PlaceholderURN != "urn:li:image:<to-be-uploaded>" {
		t.Fatalf("placeholder_urn = %q", payload.Data.WouldPost.WouldUpload.PlaceholderURN)
	}
}

func TestPostCreateImageMissingFileFails(t *testing.T) {
	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "image post", "--image", "/nonexistent-file-xyz.png"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing image file, stderr=%s", stderr)
	}
}

func TestPostCreateWithExpiredSessionFailsAuth(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "stale",
		MemberURN:   "urn:li:person:abc123",
		ExpiresAt:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "Hello expired"},
		testDepsOptions{store: store})
	if code != 4 {
		t.Fatalf("expected exit 4, got %d stderr=%s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}
