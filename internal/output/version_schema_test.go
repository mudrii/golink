package output

func versionSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "version",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_version_01",
						"command": "version",
						"transport": "official",
						"generated_at": "2026-04-16T10:33:00Z",
						"data": {
							"version": "0.1.0",
							"go_version": "go1.26.2",
							"os": "darwin",
							"arch": "arm64",
							"commit": "abc1234",
							"build_date": "2026-04-16"
						}
					}`),
		},
	}
}
