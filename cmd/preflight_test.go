package cmd

import "testing"

func TestPreflightFlagsParsesJSONAndTransport(t *testing.T) {
	for _, tc := range []struct {
		name          string
		args          []string
		env           map[string]string
		wantJSON      bool
		wantTransport string
	}{
		{"defaults", nil, nil, false, "official"},
		{"json long", []string{"--json", "post", "list"}, nil, true, "official"},
		{"json equals true", []string{"--json=true"}, nil, true, "official"},
		{"json equals false", []string{"--json=false"}, nil, false, "official"},
		{"transport unofficial", []string{"--transport", "unofficial"}, nil, false, "unofficial"},
		{"transport equals auto", []string{"--transport=auto"}, nil, false, "auto"},
		{"transport invalid falls back", []string{"--transport=broken"}, nil, false, "official"},
		{"env json", nil, map[string]string{"GOLINK_JSON": "1"}, true, "official"},
		{"env transport", nil, map[string]string{"GOLINK_TRANSPORT": "auto"}, false, "auto"},
		{"flag overrides env", []string{"--transport=official"}, map[string]string{"GOLINK_TRANSPORT": "auto"}, false, "official"},
		{"unknown flags ignored", []string{"--bogus", "--json"}, nil, true, "official"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			gotJSON, gotTransport := preflightFlags(tc.args)
			if gotJSON != tc.wantJSON {
				t.Fatalf("json: want %v, got %v", tc.wantJSON, gotJSON)
			}
			if gotTransport != tc.wantTransport {
				t.Fatalf("transport: want %q, got %q", tc.wantTransport, gotTransport)
			}
		})
	}
}
