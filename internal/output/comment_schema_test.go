package output

func commentSchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
