package api

import (
	"testing"

	"github.com/mudrii/golink/internal/output"
)

func TestNewNoopTransportAndFeatureUnavailableErrors(t *testing.T) {
	t.Parallel()
	now := NewNoopTransport("unofficial", "official")
	if now == nil {
		t.Fatal("NewNoopTransport returned nil")
	}
	if got := now.Name(); got != "unofficial" {
		t.Fatalf("Name = %q", got)
	}

	type unavailableCase struct {
		name string
		call func() error
	}
	cases := []unavailableCase{
		{name: "profile me", call: func() error { _, err := now.ProfileMe(t.Context()); return err }},
		{name: "create post", call: func() error { _, err := now.CreatePost(t.Context(), CreatePostRequest{}); return err }},
		{name: "list posts", call: func() error { _, err := now.ListPosts(t.Context(), "urn:li:person:1", 10, 0); return err }},
		{name: "get post", call: func() error { _, err := now.GetPost(t.Context(), "urn:li:share:1"); return err }},
		{name: "delete post", call: func() error { _, err := now.DeletePost(t.Context(), "urn:li:share:1"); return err }},
		{name: "add comment", call: func() error { _, err := now.AddComment(t.Context(), "urn:li:share:1", "hello"); return err }},
		{name: "list comments", call: func() error { _, err := now.ListComments(t.Context(), "urn:li:share:1", 10, 0); return err }},
		{name: "add reaction", call: func() error {
			_, err := now.AddReaction(t.Context(), "urn:li:share:1", output.ReactionLike)
			return err
		}},
		{name: "list reactions", call: func() error { _, err := now.ListReactions(t.Context(), "urn:li:share:1"); return err }},
		{name: "initialize image upload", call: func() error {
			_, _, err := now.InitializeImageUpload(t.Context(), "urn:li:person:1")
			return err
		}},
		{name: "upload image", call: func() error { return now.UploadImageBinary(t.Context(), "https://u", "/tmp/x.png") }},
		{name: "edit post", call: func() error { _, err := now.EditPost(t.Context(), EditPostRequest{}); return err }},
		{name: "reshare post", call: func() error { _, err := now.ResharePost(t.Context(), ResharePostRequest{}); return err }},
		{name: "search people", call: func() error { _, err := now.SearchPeople(t.Context(), SearchPeopleRequest{}); return err }},
		{name: "social metadata", call: func() error {
			_, err := now.SocialMetadata(t.Context(), []string{"urn:li:share:1"})
			return err
		}},
		{name: "list organizations", call: func() error { _, err := now.ListOrganizations(t.Context()); return err }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			feature, ok := AsFeatureUnavailable(err)
			if !ok {
				t.Fatalf("expected ErrFeatureUnavailable, got %T %v", err, err)
			}
			if feature.SuggestedTransport != "official" {
				t.Fatalf("suggested = %q", feature.SuggestedTransport)
			}
		})
	}
}
