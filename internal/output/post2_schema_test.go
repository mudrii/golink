package output

func post2SchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
