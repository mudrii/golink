package cmd

import "testing"

func TestPreflightFlagsParsesJSONAndTransport(t *testing.T) {
	for _, tc := range []struct {
		name           string
		args           []string
		env            map[string]string
		wantJSON       bool
		wantTransport  string
		wantOutputMode string
	}{
		{"defaults", nil, nil, false, "official", "text"},
		{"json long", []string{"--json", "post", "list"}, nil, true, "official", "json"},
		{"json equals true", []string{"--json=true"}, nil, true, "official", "json"},
		{"json equals false", []string{"--json=false"}, nil, false, "official", "text"},
		{"transport unofficial", []string{"--transport", "unofficial"}, nil, false, "unofficial", "text"},
		{"transport equals auto", []string{"--transport=auto"}, nil, false, "auto", "text"},
		{"transport invalid falls back", []string{"--transport=broken"}, nil, false, "official", "text"},
		{"env json", nil, map[string]string{"GOLINK_JSON": "1"}, true, "official", "json"},
		{"env transport", nil, map[string]string{"GOLINK_TRANSPORT": "auto"}, false, "auto", "text"},
		{"flag overrides env", []string{"--transport=official"}, map[string]string{"GOLINK_TRANSPORT": "auto"}, false, "official", "text"},
		{"unknown flags ignored", []string{"--bogus", "--json"}, nil, true, "official", "json"},
		{"output compact", []string{"--output=compact"}, nil, false, "official", "compact"},
		{"compact flag", []string{"--compact"}, nil, false, "official", "compact"},
		{"compact beats output", []string{"--compact", "--output=compact"}, nil, false, "official", "compact"},
		{"output jsonl", []string{"--output=jsonl"}, nil, false, "official", "jsonl"},
		{"output table", []string{"--output=table"}, nil, false, "official", "table"},
		{"output json flag", []string{"--output=json"}, nil, false, "official", "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			gotJSON, gotTransport, gotOutputMode := preflightFlags(tc.args)
			if gotJSON != tc.wantJSON {
				t.Fatalf("json: want %v, got %v", tc.wantJSON, gotJSON)
			}
			if gotTransport != tc.wantTransport {
				t.Fatalf("transport: want %q, got %q", tc.wantTransport, gotTransport)
			}
			if gotOutputMode != tc.wantOutputMode {
				t.Fatalf("outputMode: want %q, got %q", tc.wantOutputMode, gotOutputMode)
			}
		})
	}
}
