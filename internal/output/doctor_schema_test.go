package output

func doctorSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "doctor",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_doctor_01",
						"command": "doctor",
						"transport": "official",
						"generated_at": "2026-04-17T12:00:00Z",
						"data": {
							"api_version": "202604",
							"environment": {
								"golink_client_id_set": true,
								"golink_api_version": "202604",
								"config_loaded": false
							},
							"session": {
								"profile": "default",
								"authenticated": true,
								"expires_at": "2026-06-17T12:00:00Z",
								"expires_in_hours": 1440,
								"refresh_available": true,
								"refresh_expires_at": "2027-04-17T12:00:00Z",
								"refresh_in_days": 365,
								"scopes": ["openid", "profile", "email", "w_member_social_feed"],
								"auth_flow": "pkce",
								"connected_at": "2026-04-10T12:00:00Z"
							},
							"probe": {
								"url": "https://api.linkedin.com/v2/userinfo",
								"status": 200,
								"member_urn": "urn:li:person:abc123",
								"request_id": "abc-request-id",
								"attempted": true
							},
							"features": [
								{"command": "profile me", "status": "supported"},
								{"command": "post create", "status": "supported"},
								{"command": "post list", "status": "unsupported", "reason": "r_member_social is closed by LinkedIn (entitlement-gated)"},
								{"command": "auth refresh", "status": "supported"}
							],
							"audit": {
								"path": "/home/user/.local/state/golink/audit.jsonl",
								"enabled": true,
								"exists": true,
								"size": 1024,
								"modified_at": "2026-04-16T10:33:00Z"
							},
							"health": "ok"
						}
					}`),
		},
	}
}
