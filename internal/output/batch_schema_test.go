package output

func batchSchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
