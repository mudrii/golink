package output

func reactSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "react add dry run",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_react_add_dryrun_01",
						"command": "react add",
						"transport": "official",
						"mode": "dry_run",
						"generated_at": "2026-04-16T10:23:30Z",
						"data": {
							"would_react": {
								"endpoint": "POST /rest/reactions",
								"post_urn": "urn:li:share:42",
								"type": "PRAISE"
							},
							"mode": "dry_run"
						}
					}`),
		},
	}
}
