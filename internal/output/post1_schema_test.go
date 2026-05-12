package output

func post1SchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
