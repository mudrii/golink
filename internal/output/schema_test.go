package output

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const defaultSchemaPath = "../../schemas/golink-output.schema.json"

// Test fixtures for every command output and error envelope shape described in PROMPT_golink.md v3.
type schemaFixture struct {
	name    string
	payload []byte
}

func TestGolinkOutputSchemaRoundTrips(t *testing.T) {
	fixtures := []schemaFixture{
		{
			name: "auth status",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_auth_status_01",
				"command": "auth status",
				"transport": "official",
				"generated_at": "2026-04-16T10:00:00Z",
				"data": {
					"is_authenticated": true,
					"profile": "default",
					"transport": "official",
					"scopes": ["openid", "profile", "email", "w_member_social"],
					"expires_at": "2026-04-16T11:00:00Z",
					"refresh_expires_at": "2027-04-16T10:00:00Z",
					"auth_flow": "pkce"
				}
			}`),
		},
		{
			name: "auth refresh",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_auth_refresh_01",
				"command": "auth refresh",
				"transport": "official",
				"generated_at": "2026-04-16T10:05:00Z",
				"data": {
					"profile": "default",
					"transport": "official",
					"refreshed_at": "2026-04-16T10:05:00Z",
					"expires_at": "2026-06-15T10:05:00Z",
					"refresh_expires_at": "2027-04-16T10:00:00Z",
					"scopes_granted": ["openid", "profile", "email", "w_member_social"]
				}
			}`),
		},
		{
			name: "auth login",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_auth_login_01",
				"command": "auth login",
				"transport": "official",
				"generated_at": "2026-04-16T10:00:00Z",
				"data": {
					"url": "https://www.linkedin.com/oauth/native-pkce/authorization?client_id=abc",
					"profile": "default",
					"transport": "official",
					"timeout_ms": 120000
				}
			}`),
		},
		{
			name: "auth login result",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_auth_login_result_01",
				"command": "auth login",
				"transport": "official",
				"generated_at": "2026-04-16T10:05:00Z",
				"data": {
					"status": "success",
					"profile": "default",
					"transport": "official",
					"connected_at": "2026-04-16T10:05:00Z",
					"scopes_granted": ["openid", "profile", "email", "w_member_social"]
				}
			}`),
		},
		{
			name: "auth logout",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_auth_logout_01",
				"command": "auth logout",
				"transport": "official",
				"generated_at": "2026-04-16T10:10:00Z",
				"data": {
					"status": "ok",
					"profile": "default",
					"transport": "official",
					"cleared": true
				}
			}`),
		},
		{
			name: "profile me",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_profile_me_01",
				"command": "profile me",
				"transport": "official",
				"generated_at": "2026-04-16T10:11:00Z",
				"data": {
					"sub": "urn:li:person:abc123",
					"name": "Ion Mudreac",
					"email": "ion@example.com",
					"picture": "https://media.licdn.com/example.jpg",
					"locale": {
						"country": "MY",
						"language": "en"
					},
					"profile_id": "abc123"
				}
			}`),
		},
		{
			name: "post create",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_create_01",
				"command": "post create",
				"transport": "official",
				"generated_at": "2026-04-16T10:20:00Z",
				"data": {
					"id": "urn:li:share:7123456789",
					"created_at": "2026-04-16T10:20:00Z",
					"text": "Hello world",
					"visibility": "PUBLIC",
					"url": "https://www.linkedin.com/feed/update/urn:li:share:7123456789",
					"author_urn": "urn:li:person:abc123"
				}
			}`),
		},
		{
			name: "post create dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_create_dryrun_01",
				"command": "post create",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-16T10:21:00Z",
				"data": {
					"would_post": {
						"endpoint": "POST /rest/posts",
						"text": "Hello",
						"visibility": "PUBLIC"
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "post delete dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_delete_dryrun_01",
				"command": "post delete",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-16T10:21:30Z",
				"data": {
					"would_delete": {
						"endpoint": "DELETE /rest/posts/urn:li:share:42",
						"post_urn": "urn:li:share:42"
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "comment add dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_comment_add_dryrun_01",
				"command": "comment add",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-16T10:22:30Z",
				"data": {
					"would_comment": {
						"endpoint": "POST /rest/socialActions/urn:li:share:42/comments",
						"post_urn": "urn:li:share:42",
						"text": "nice"
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "react add dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_react_add_dryrun_01",
				"command": "react add",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-16T10:23:30Z",
				"data": {
					"would_react": {
						"endpoint": "POST /rest/reactions",
						"post_urn": "urn:li:share:42",
						"type": "PRAISE"
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "post list",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_list_01",
				"command": "post list",
				"transport": "official",
				"generated_at": "2026-04-16T10:22:00Z",
				"data": {
					"owner_urn": "urn:li:person:abc123",
					"count": 2,
					"start": 0,
					"items": [
						{
							"id": "urn:li:share:1",
							"created_at": "2026-04-16T09:00:00Z",
							"text": "first post",
							"visibility": "PUBLIC",
							"url": "https://www.linkedin.com/feed/update/urn:li:share:1",
							"author_urn": "urn:li:person:abc123",
							"like_count": 1,
							"comment_count": 2
						},
						{
							"id": "urn:li:share:2",
							"created_at": "2026-04-16T09:05:00Z",
							"text": "second post",
							"visibility": "CONNECTIONS",
							"url": "https://www.linkedin.com/feed/update/urn:li:share:2",
							"author_urn": "urn:li:person:abc123"
						}
					]
				}
			}`),
		},
		{
			name: "post get",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_get_01",
				"command": "post get",
				"transport": "official",
				"generated_at": "2026-04-16T10:23:00Z",
				"data": {
					"id": "urn:li:share:1",
					"created_at": "2026-04-16T09:00:00Z",
					"text": "first post",
					"visibility": "PUBLIC",
					"url": "https://www.linkedin.com/feed/update/urn:li:share:1",
					"author_urn": "urn:li:person:abc123",
					"like_count": 3,
					"comment_count": 0,
					"publish_time": 1713254400
				}
			}`),
		},
		{
			name: "post delete",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_delete_01",
				"command": "post delete",
				"transport": "official",
				"generated_at": "2026-04-16T10:24:00Z",
				"data": {
					"id": "urn:li:share:2",
					"deleted": true,
					"revisions": 1
				}
			}`),
		},
		{
			name: "comment add",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_comment_add_01",
				"command": "comment add",
				"transport": "official",
				"generated_at": "2026-04-16T10:25:00Z",
				"data": {
					"id": "urn:li:comment:123",
					"post_urn": "urn:li:share:1",
					"author": "urn:li:person:abc123",
					"text": "Nice post!",
					"created_at": "2026-04-16T10:25:00Z",
					"likeable": true
				}
			}`),
		},
		{
			name: "comment list",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_comment_list_01",
				"command": "comment list",
				"transport": "official",
				"generated_at": "2026-04-16T10:26:00Z",
				"data": {
					"post_urn": "urn:li:share:1",
					"items": [
						{
							"id": "urn:li:comment:123",
							"post_urn": "urn:li:share:1",
							"author": "urn:li:person:abc123",
							"text": "Great",
							"created_at": "2026-04-16T10:25:00Z",
							"likeable": false
						}
					],
					"count": 1,
					"start": 0
				}
			}`),
		},
		{
			name: "reaction add",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_reaction_add_01",
				"command": "react add",
				"transport": "official",
				"generated_at": "2026-04-16T10:27:00Z",
				"data": {
					"post_urn": "urn:li:share:1",
					"actor_urn": "urn:li:person:abc123",
					"type": "LIKE",
					"at": "2026-04-16T10:27:00Z",
					"target_urn": "urn:li:share:1"
				}
			}`),
		},
		{
			name: "reaction list",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_reaction_list_01",
				"command": "react list",
				"transport": "official",
				"generated_at": "2026-04-16T10:28:00Z",
				"data": {
					"post_urn": "urn:li:share:1",
					"items": [
						{"post_urn":"urn:li:share:1","actor_urn":"urn:li:person:abc123","type":"LIKE","at":"2026-04-16T10:27:00Z"},
						{"post_urn":"urn:li:share:1","actor_urn":"urn:li:person:def456","type":"EMPATHY","at":"2026-04-16T10:28:00Z"}
					],
					"count": 2
				}
			}`),
		},
		{
			name: "search people",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_search_people_01",
				"command": "search people",
				"transport": "unofficial",
				"mode": "normal",
				"generated_at": "2026-04-16T10:29:00Z",
				"data": {
					"query": "engineer in san jose",
					"count": 1,
					"start": 0,
					"total_count": 1,
					"people": [
						{
							"urn": "urn:li:person:def456",
							"full_name": "Taylor Engineer",
							"headline": "Platform Architect",
							"location": "San Jose, CA",
							"industry": "Technology",
							"profile_picture": "https://media.licdn.com/person.jpg",
							"skills": ["Go", "Distributed systems"]
						}
					]
				}
			}`),
		},
		{
			name: "doctor",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_doctor_01",
				"command": "doctor",
				"transport": "official",
				"generated_at": "2026-04-17T12:00:00Z",
				"data": {
					"api_version": "202504",
					"environment": {
						"golink_client_id_set": true,
						"golink_api_version": "202504",
						"config_loaded": false
					},
					"session": {
						"profile": "default",
						"authenticated": true,
						"expires_at": "2026-06-17T12:00:00Z",
						"expires_in_hours": 1440,
						"refresh_available": true,
						"refresh_expires_at": "2027-04-17T12:00:00Z",
						"refresh_in_days": 365,
						"scopes": ["openid", "profile", "email", "w_member_social"],
						"auth_flow": "pkce",
						"connected_at": "2026-04-10T12:00:00Z"
					},
					"probe": {
						"url": "https://api.linkedin.com/v2/userinfo",
						"status": 200,
						"member_urn": "urn:li:person:abc123",
						"request_id": "abc-request-id",
						"attempted": true
					},
					"features": [
						{"command": "profile me", "status": "supported"},
						{"command": "post create", "status": "supported"},
						{"command": "post list", "status": "unsupported", "reason": "r_member_social is closed by LinkedIn (entitlement-gated)"},
						{"command": "auth refresh", "status": "supported"}
					],
					"audit": {
						"path": "/home/user/.local/state/golink/audit.jsonl",
						"enabled": true,
						"exists": true,
						"size": 1024,
						"modified_at": "2026-04-16T10:33:00Z"
					},
					"health": "ok"
				}
			}`),
		},
		{
			name: "validation error",
			payload: []byte(`{
				"status": "validation_error",
				"command_id": "cmd_validation_error_01",
				"command": "post create",
				"transport": "official",
				"generated_at": "2026-04-16T10:29:30Z",
				"error": "missing required flag: --text",
				"code": "VALIDATION_ERROR",
				"details": "non-interactive mode requires --text"
			}`),
		},
		{
			name: "version",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_version_01",
				"command": "version",
				"transport": "official",
				"generated_at": "2026-04-16T10:33:00Z",
				"data": {
					"version": "0.1.0",
					"go_version": "go1.26.2",
					"os": "darwin",
					"arch": "arm64",
					"commit": "abc1234",
					"build_date": "2026-04-16"
				}
			}`),
		},
		{
			name: "unsupported",
			payload: []byte(`{
				"status": "unsupported",
				"command_id": "cmd_unsupported_01",
				"command": "search people",
				"transport": "official",
				"generated_at": "2026-04-16T10:31:00Z",
				"data": {
					"feature": "search people",
					"reason": "not available through open self-serve LinkedIn consumer/community permissions",
					"suggested_fallback": "--transport=unofficial"
				}
			}`),
		},
		{
			name: "error",
			payload: []byte(`{
				"status": "error",
				"command_id": "cmd_error_01",
				"command": "profile me",
				"transport": "official",
				"generated_at": "2026-04-16T10:32:00Z",
				"error": "Token expired or invalid. Re-run: golink auth login",
				"code": "UNAUTHORIZED",
				"details": "token_expired"
			}`),
		},
		{
			name: "batch op result",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_batch_01",
				"command": "batch",
				"transport": "official",
				"generated_at": "2026-04-17T12:00:00Z",
				"data": {
					"line": 1,
					"status": "ok",
					"command": "post create",
					"idempotency_key": "test-1",
					"command_id": "cmd_post_create_xxx",
					"http_status": 201,
					"from_cache": false,
					"data": {"id": "urn:li:share:1"}
				}
			}`),
		},
		{
			name: "batch op result from cache",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_batch_02",
				"command": "batch",
				"transport": "official",
				"generated_at": "2026-04-17T12:01:00Z",
				"from_cache": true,
				"data": {
					"line": 2,
					"status": "ok",
					"command": "post create",
					"idempotency_key": "test-1",
					"command_id": "cmd_post_create_xxx",
					"http_status": 201,
					"from_cache": true
				}
			}`),
		},
		{
			name: "approval pending",
			payload: []byte(`{
				"status": "pending_approval",
				"command_id": "cmd_post_create_approv01",
				"command": "post create",
				"transport": "official",
				"generated_at": "2026-04-17T12:34:56Z",
				"data": {
					"command_id": "cmd_post_create_approv01",
					"command": "post create",
					"staged_at": "2026-04-17T12:34:56Z",
					"staged_path": "/home/user/.local/state/golink/approvals/cmd_post_create_approv01.pending.json",
					"payload": {
						"endpoint": "POST /rest/posts",
						"text": "approval test",
						"visibility": "PUBLIC"
					},
					"idempotency_key": "key-1"
				}
			}`),
		},
		{
			name: "approval list",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_approval_list_01",
				"command": "approval list",
				"transport": "official",
				"generated_at": "2026-04-17T12:35:00Z",
				"data": {
					"items": [
						{
							"command_id": "cmd_post_create_approv01",
							"command": "post create",
							"state": "pending",
							"staged_at": "2026-04-17T12:34:56Z",
							"idempotency_key": "key-1"
						},
						{
							"command_id": "cmd_post_delete_approv01",
							"command": "post delete",
							"state": "approved",
							"staged_at": "2026-04-17T12:33:00Z"
						}
					]
				}
			}`),
		},
		{
			name: "approval show",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_approval_show_01",
				"command": "approval show",
				"transport": "official",
				"generated_at": "2026-04-17T12:36:00Z",
				"data": {
					"entry": {
						"command_id": "cmd_post_create_approv01",
						"command": "post create",
						"created_at": "2026-04-17T12:34:56Z",
						"transport": "official",
						"profile": "default",
						"payload": {"endpoint": "POST /rest/posts", "text": "hello", "visibility": "PUBLIC"}
					},
					"state": "pending"
				}
			}`),
		},
		{
			name: "approval state change",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_approval_grant_01",
				"command": "approval grant",
				"transport": "official",
				"generated_at": "2026-04-17T12:37:00Z",
				"data": {
					"command_id": "cmd_post_create_approv01",
					"state": "approved"
				}
			}`),
		},
		{
			name: "social metadata",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_social_metadata_01",
				"command": "social metadata",
				"transport": "official",
				"generated_at": "2026-04-17T12:00:00Z",
				"data": {
					"items": [
						{
							"post_urn": "urn:li:share:1",
							"like_count": 12,
							"comment_count": 3,
							"all_comment_count": 5,
							"reaction_count": 15,
							"reaction_counts": {"LIKE": 12, "PRAISE": 3},
							"comments_state": "ENABLED"
						},
						{
							"post_urn": "urn:li:share:2",
							"like_count": 0,
							"comment_count": 0,
							"reaction_count": 0,
							"error": "status 404: not found"
						}
					],
					"count": 2
				}
			}`),
		},
		{
			name: "post edit",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_edit_01",
				"command": "post edit",
				"transport": "official",
				"generated_at": "2026-04-17T12:00:00Z",
				"data": {
					"id": "urn:li:share:42",
					"created_at": "2026-04-16T09:00:00Z",
					"text": "updated text",
					"visibility": "PUBLIC",
					"url": "https://www.linkedin.com/feed/update/urn:li:share:42",
					"author_urn": "urn:li:person:abc123",
					"updated_at": "2026-04-17T12:00:00Z"
				}
			}`),
		},
		{
			name: "post edit dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_edit_dryrun_01",
				"command": "post edit",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-17T12:01:00Z",
				"data": {
					"would_patch": {
						"endpoint": "PATCH /rest/posts/urn:li:share:42",
						"post_urn": "urn:li:share:42",
						"patch": {"$set": {"commentary": "new text"}}
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "post reshare",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_reshare_01",
				"command": "post reshare",
				"transport": "official",
				"generated_at": "2026-04-17T12:02:00Z",
				"data": {
					"id": "urn:li:share:99",
					"created_at": "2026-04-17T12:02:00Z",
					"text": "worth sharing",
					"visibility": "PUBLIC",
					"url": "https://www.linkedin.com/feed/update/urn:li:share:99",
					"author_urn": "urn:li:person:abc123"
				}
			}`),
		},
		{
			name: "post reshare dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_reshare_dryrun_01",
				"command": "post reshare",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-17T12:03:00Z",
				"data": {
					"would_reshare": {
						"endpoint": "POST /rest/posts",
						"parent_urn": "urn:li:share:1",
						"commentary": "worth sharing",
						"visibility": "PUBLIC"
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "post create with image dry run",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_post_create_img_dryrun_01",
				"command": "post create",
				"transport": "official",
				"mode": "dry_run",
				"generated_at": "2026-04-17T12:04:00Z",
				"data": {
					"would_post": {
						"endpoint": "POST /rest/posts",
						"text": "image post",
						"visibility": "PUBLIC",
						"would_upload": {
							"path": "/tmp/photo.jpg",
							"placeholder_urn": "urn:li:image:<to-be-uploaded>",
							"alt": "a nice photo"
						}
					},
					"mode": "dry_run"
				}
			}`),
		},
		{
			name: "success with rate limit metadata",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_profile_me_02",
				"command": "profile me",
				"transport": "official",
				"generated_at": "2026-04-16T10:34:00Z",
				"rate_limit": {
					"remaining": 42,
					"reset_at": "2026-04-16T11:00:00Z"
				},
				"data": {
					"sub": "urn:li:person:abc123",
					"name": "Ion Mudreac",
					"email": "ion@example.com",
					"picture": "https://media.licdn.com/example.jpg",
					"locale": {
						"country": "MY",
						"language": "en"
					}
				}
			}`),
		},
	}

	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			ValidateEnvelopeRoundTrip(t, defaultSchemaPath, tc.payload)
		})
	}
}

// ensure default schema path exists and is an actual file at runtime
func TestSchemaFileExists(t *testing.T) {
	path := defaultSchemaPath
	if !filepath.IsAbs(path) {
		if cwd, err := os.Getwd(); err == nil {
			path = filepath.Clean(filepath.Join(cwd, path))
		}
	}
	if _, err := os.Stat(path); err != nil {
		if err, ok := err.(*fs.PathError); ok {
			t.Fatalf("schema file missing: %v", err)
		}
		t.Fatalf("schema file check failed: %v", err)
	}
}
