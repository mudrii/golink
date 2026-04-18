package api

import (
	"context"
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
		{name: "profile me", call: func() error { _, err := now.ProfileMe(context.Background()); return err }},
		{name: "create post", call: func() error { _, err := now.CreatePost(context.Background(), CreatePostRequest{}); return err }},
		{name: "list posts", call: func() error { _, err := now.ListPosts(context.Background(), "urn:li:person:1", 10, 0); return err }},
		{name: "get post", call: func() error { _, err := now.GetPost(context.Background(), "urn:li:share:1"); return err }},
		{name: "delete post", call: func() error { _, err := now.DeletePost(context.Background(), "urn:li:share:1"); return err }},
		{name: "add comment", call: func() error { _, err := now.AddComment(context.Background(), "urn:li:share:1", "hello"); return err }},
		{name: "list comments", call: func() error { _, err := now.ListComments(context.Background(), "urn:li:share:1", 10, 0); return err }},
		{name: "add reaction", call: func() error {
			_, err := now.AddReaction(context.Background(), "urn:li:share:1", output.ReactionLike)
			return err
		}},
		{name: "list reactions", call: func() error { _, err := now.ListReactions(context.Background(), "urn:li:share:1"); return err }},
		{name: "initialize image upload", call: func() error {
			_, _, err := now.InitializeImageUpload(context.Background(), "urn:li:person:1")
			return err
		}},
		{name: "upload image", call: func() error { return now.UploadImageBinary(context.Background(), "https://u", "/tmp/x.png") }},
		{name: "edit post", call: func() error { _, err := now.EditPost(context.Background(), EditPostRequest{}); return err }},
		{name: "reshare post", call: func() error { _, err := now.ResharePost(context.Background(), ResharePostRequest{}); return err }},
		{name: "search people", call: func() error { _, err := now.SearchPeople(context.Background(), SearchPeopleRequest{}); return err }},
		{name: "social metadata", call: func() error {
			_, err := now.SocialMetadata(context.Background(), []string{"urn:li:share:1"})
			return err
		}},
		{name: "list organizations", call: func() error { _, err := now.ListOrganizations(context.Background()); return err }},
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
