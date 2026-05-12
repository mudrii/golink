package output

func profileSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "profile me",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_profile_me_01",
						"command": "profile me",
						"transport": "official",
						"generated_at": "2026-04-16T10:11:00Z",
						"data": {
							"sub": "urn:li:person:abc123",
							"name": "Ion Mudreac",
							"email": "ion@example.com",
							"picture": "https://media.licdn.com/example.jpg",
							"locale": {
								"country": "MY",
								"language": "en"
							},
							"profile_id": "abc123"
						}
					}`),
		},
	}
}
