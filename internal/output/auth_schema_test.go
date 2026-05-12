package output

func authSchemaFixtures() []schemaFixture {
	return []schemaFixture{
		{
			name: "auth status",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_auth_status_01",
						"command": "auth status",
						"transport": "official",
						"generated_at": "2026-04-16T10:00:00Z",
						"data": {
							"is_authenticated": true,
							"profile": "default",
							"transport": "official",
							"scopes": ["openid", "profile", "email", "w_member_social_feed"],
							"expires_at": "2026-04-16T11:00:00Z",
							"refresh_expires_at": "2027-04-16T10:00:00Z",
							"auth_flow": "pkce"
						}
					}`),
		},
		{
			name: "auth refresh",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_auth_refresh_01",
						"command": "auth refresh",
						"transport": "official",
						"generated_at": "2026-04-16T10:05:00Z",
						"data": {
							"profile": "default",
							"transport": "official",
							"refreshed_at": "2026-04-16T10:05:00Z",
							"expires_at": "2026-06-15T10:05:00Z",
							"refresh_expires_at": "2027-04-16T10:00:00Z",
							"scopes_granted": ["openid", "profile", "email", "w_member_social_feed"]
						}
					}`),
		},
		{
			name: "auth login",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_auth_login_01",
						"command": "auth login",
						"transport": "official",
						"generated_at": "2026-04-16T10:00:00Z",
						"data": {
							"type": "login_url",
							"url": "https://www.linkedin.com/oauth/native-pkce/authorization?client_id=abc",
							"profile": "default",
							"transport": "official",
							"timeout_ms": 120000
						}
					}`),
		},
		{
			name: "auth login result",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_auth_login_result_01",
						"command": "auth login",
						"transport": "official",
						"generated_at": "2026-04-16T10:05:00Z",
						"data": {
							"type": "login_result",
							"status": "success",
							"profile": "default",
							"transport": "official",
							"connected_at": "2026-04-16T10:05:00Z",
							"scopes_granted": ["openid", "profile", "email", "w_member_social_feed"]
						}
					}`),
		},
		{
			name: "auth logout",
			payload: []byte(`{
						"status": "ok",
						"command_id": "cmd_auth_logout_01",
						"command": "auth logout",
						"transport": "official",
						"generated_at": "2026-04-16T10:10:00Z",
						"data": {
							"status": "ok",
							"profile": "default",
							"transport": "official",
							"cleared": true
						}
					}`),
		},
	}
}
