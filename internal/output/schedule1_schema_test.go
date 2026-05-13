package output

func schedule1SchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "schedule post create",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_post_schedule_01",
						"command": "post schedule",
						"transport": "official",
						"generated_at": "2026-04-17T12:34:56Z",
						"data": {
							"command_id": "cmd_post_schedule_01",
							"state": "pending",
							"scheduled_at": "2027-01-01T09:00:00Z",
							"created_at": "2026-04-17T12:34:56Z",
							"profile": "default",
							"transport": "official",
							"request": {
								"text": "scheduled post text",
								"visibility": "PUBLIC"
							}
						}
					}`),
		},
		{
			name: "schedule list",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_schedule_list_01",
						"command": "schedule list",
						"transport": "official",
						"generated_at": "2026-04-17T12:35:00Z",
						"data": {
							"items": [
								{
									"command_id": "cmd_post_schedule_01",
									"state": "pending",
									"scheduled_at": "2027-01-01T09:00:00Z",
									"created_at": "2026-04-17T12:34:56Z",
									"retry_count": 0,
									"profile": "default",
									"transport": "official",
									"request": {
										"text": "scheduled post text",
										"visibility": "PUBLIC"
									}
								},
								{
									"command_id": "cmd_post_schedule_02",
									"state": "failed",
									"scheduled_at": "2026-04-17T10:00:00Z",
									"created_at": "2026-04-17T09:00:00Z",
									"last_error": "transport error: 500",
									"retry_count": 2,
									"profile": "default",
									"transport": "official",
									"request": {
										"text": "failed post",
										"visibility": "CONNECTIONS"
									}
								}
							],
							"counts": {
								"pending": 1,
								"past_due": 0,
								"completed": 0,
								"failed": 1,
								"cancelled": 0
							}
						}
					}`),
		},
		{
			name: "schedule run",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_schedule_run_01",
						"command": "schedule run",
						"transport": "official",
						"generated_at": "2026-04-17T12:36:00Z",
						"data": {
							"ran": 2,
							"succeeded": 1,
							"failed": 1,
							"skipped": 0,
							"results": [
								{
									"command_id": "cmd_post_schedule_01",
									"status": "succeeded",
									"post_urn": "urn:li:share:123"
								},
								{
									"command_id": "cmd_post_schedule_02",
									"status": "failed",
									"error": "transport error: 500"
								}
							]
						}
					}`),
		},
		{
			// Mixed-state run: one succeeded, one failed, one skipped. Existing
			// fixtures cover the all-success and all-fail edges; this guards
			// the array-item heterogeneity against future regressions.
			name: "schedule run mixed",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_schedule_run_mixed_01",
						"command": "schedule run",
						"transport": "official",
						"generated_at": "2026-04-17T12:36:30Z",
						"data": {
							"ran": 3,
							"succeeded": 1,
							"failed": 1,
							"skipped": 1,
							"results": [
								{
									"command_id": "cmd_post_schedule_a",
									"status": "succeeded",
									"post_urn": "urn:li:share:789"
								},
								{
									"command_id": "cmd_post_schedule_b",
									"status": "failed",
									"error": "transport error: 502"
								},
								{
									"command_id": "cmd_post_schedule_c",
									"status": "skipped"
								}
							]
						}
					}`),
		},
		{
			name: "schedule cancel",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_schedule_cancel_01",
						"command": "schedule cancel",
						"transport": "official",
						"generated_at": "2026-04-17T12:37:00Z",
						"data": {
							"command_id": "cmd_post_schedule_01",
							"state": "cancelled",
							"scheduled_at": "2027-01-01T09:00:00Z",
							"created_at": "2026-04-17T12:34:56Z",
							"profile": "default",
							"transport": "official",
							"request": {
								"text": "scheduled post text",
								"visibility": "PUBLIC"
							}
						}
					}`),
		},
		{
			name: "schedule show",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_schedule_show_01",
						"command": "schedule show",
						"transport": "official",
						"generated_at": "2026-04-17T12:38:00Z",
						"data": {
							"command_id": "cmd_post_schedule_01",
							"state": "pending",
							"scheduled_at": "2027-01-01T09:00:00Z",
							"created_at": "2026-04-17T12:34:56Z",
							"retry_count": 0,
							"profile": "default",
							"transport": "official",
							"request": {
								"text": "scheduled post text",
								"visibility": "PUBLIC",
								"image_path": "/home/user/photos/image.jpg",
								"image_alt": "a photo"
							},
							"idempotency_key": "sched-key-1"
						}
					}`),
		},
	}
}
