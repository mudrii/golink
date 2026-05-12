package output

func orgSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "org list",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_org_list_01",
						"command": "org list",
						"transport": "official",
						"generated_at": "2026-04-17T12:00:00Z",
						"data": {
							"count": 2,
							"items": [
								{
									"urn": "urn:li:organization:111",
									"role": "ADMINISTRATOR",
									"state": "APPROVED",
									"name": "Acme Corp",
									"vanity_name": "acmecorp",
									"logo_url": "https://media.licdn.com/logos/acme.png"
								},
								{
									"urn": "urn:li:organization:222",
									"role": "ADMINISTRATOR",
									"state": "APPROVED",
									"name": "Beta Ltd"
								}
							]
						}
					}`),
		},
	}
}
