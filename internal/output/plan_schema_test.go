package output

func planSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "plan output",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_plan_01",
						"command": "plan",
						"transport": "official",
						"generated_at": "2026-04-17T12:00:00Z",
						"data": {
							"schema": "golink.plan/v1",
							"created_at": "2026-04-17T12:00:00Z",
							"command": "post create",
							"args": {"text": "hello", "visibility": "PUBLIC"},
							"transport": "official",
							"profile": "default"
						}
					}`),
		},
	}
}
