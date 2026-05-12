package output

func schedule2SchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "schedule next",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_schedule_next_01",
						"command": "schedule next",
						"transport": "official",
						"generated_at": "2026-04-17T12:39:00Z",
						"data": {
							"command_id": "cmd_post_schedule_01",
							"state": "pending",
							"scheduled_at": "2027-01-01T09:00:00Z",
							"created_at": "2026-04-17T12:34:56Z",
							"retry_count": 0,
							"profile": "default",
							"transport": "official",
							"request": {
								"text": "next scheduled post",
								"visibility": "PUBLIC"
							}
						}
					}`),
		},
	}
}
