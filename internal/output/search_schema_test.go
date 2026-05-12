package output

func searchSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "search people",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_search_people_01",
						"command": "search people",
						"transport": "unofficial",
						"mode": "normal",
						"generated_at": "2026-04-16T10:29:00Z",
						"data": {
							"query": "engineer in san jose",
							"count": 1,
							"start": 0,
							"total_count": 1,
							"people": [
								{
									"urn": "urn:li:person:def456",
									"full_name": "Taylor Engineer",
									"headline": "Platform Architect",
									"location": "San Jose, CA",
									"industry": "Technology",
									"profile_picture": "https://media.licdn.com/person.jpg",
									"skills": ["Go", "Distributed systems"]
								}
							]
						}
					}`),
		},
	}
}
