package output

func approvalSchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
