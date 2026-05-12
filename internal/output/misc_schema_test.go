package output

func miscSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "reaction add",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_reaction_add_01",
						"command": "react add",
						"transport": "official",
						"generated_at": "2026-04-16T10:27:00Z",
						"data": {
							"post_urn": "urn:li:share:1",
							"actor_urn": "urn:li:person:abc123",
							"type": "LIKE",
							"at": "2026-04-16T10:27:00Z",
							"target_urn": "urn:li:share:1"
						}
					}`),
		},
		{
			name: "reaction list",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_reaction_list_01",
						"command": "react list",
						"transport": "official",
						"generated_at": "2026-04-16T10:28:00Z",
						"data": {
							"post_urn": "urn:li:share:1",
							"items": [
								{"post_urn":"urn:li:share:1","actor_urn":"urn:li:person:abc123","type":"LIKE","at":"2026-04-16T10:27:00Z"},
								{"post_urn":"urn:li:share:1","actor_urn":"urn:li:person:def456","type":"EMPATHY","at":"2026-04-16T10:28:00Z"}
							],
							"count": 2
						}
					}`),
		},
		{
			name: "validation error",
			payload: []byte(`{
						"status": "validation_error",
						"command_id": "cmd_validation_error_01",
						"command": "post create",
						"transport": "official",
						"generated_at": "2026-04-16T10:29:30Z",
						"error": "missing required flag: --text",
						"code": "VALIDATION_ERROR",
						"details": "non-interactive mode requires --text"
					}`),
		},
		{
			name: "success with rate limit metadata",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_profile_me_02",
						"command": "profile me",
						"transport": "official",
						"generated_at": "2026-04-16T10:34:00Z",
						"rate_limit": {
							"remaining": 42,
							"reset_at": "2026-04-16T11:00:00Z"
						},
						"data": {
							"sub": "urn:li:person:abc123",
							"name": "Ion Mudreac",
							"email": "ion@example.com",
							"picture": "https://media.licdn.com/example.jpg",
							"locale": {
								"country": "MY",
								"language": "en"
							}
						}
					}`),
		},
	}
}
