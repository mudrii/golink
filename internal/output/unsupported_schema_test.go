package output

func unsupportedSchemaFixtures() []schemaFixture {
	return []schemaFixture{
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
	}
}
