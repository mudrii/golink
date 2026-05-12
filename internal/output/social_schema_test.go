package output

func socialSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "social metadata",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_social_metadata_01",
						"command": "social metadata",
						"transport": "official",
						"generated_at": "2026-04-17T12:00:00Z",
						"data": {
							"items": [
								{
									"post_urn": "urn:li:share:1",
									"like_count": 12,
									"comment_count": 3,
									"all_comment_count": 5,
									"reaction_count": 15,
									"reaction_counts": {"LIKE": 12, "PRAISE": 3},
									"comments_state": "ENABLED"
								},
								{
									"post_urn": "urn:li:share:2",
									"like_count": 0,
									"comment_count": 0,
									"reaction_count": 0,
									"error": "status 404: not found"
								}
							],
							"count": 2
						}
					}`),
		},
	}
}
