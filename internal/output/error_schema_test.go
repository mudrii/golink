package output

func errorSchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
