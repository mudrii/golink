package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fixedTime is a stable timestamp for test envelopes.
var fixedTime = time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)

func fixtureEnvelope(status CommandStatus, cmd string) BaseEnvelope {
	return BaseEnvelope{
		Status:      status,
		CommandID:   "cmd_test_01",
		Command:     cmd,
		Transport:   "official",
		GeneratedAt: fixedTime,
	}
}

// stubTabular is a minimal TabularData implementation for tests.
type stubTabular struct {
	headers []string
	rows    [][]string
}

func (s stubTabular) Headers() []string { return s.headers }
func (s stubTabular) Rows() [][]string  { return s.rows }

func TestValidateMode(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{ModeText, false},
		{ModeJSON, false},
		{ModeJSONL, false},
		{ModeCompact, false},
		{ModeTable, false},
		{"", true},
		{"xml", true},
		{"TEXT", true},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			err := ValidateMode(tc.mode)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRenderSuccess_JSON_scalar(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "version")
	data := VersionData{Version: "0.1.0", GoVersion: "go1.26.2", OS: "darwin", Arch: "arm64"}
	if err := RenderSuccess(&buf, ModeJSON, env, data, "v0.1.0"); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", out["status"])
	}
	if out["command_id"] == nil {
		t.Fatal("expected command_id in JSON mode")
	}
}

func TestRenderSuccess_Compact_scalar(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "version")
	data := VersionData{Version: "0.1.0"}
	if err := RenderSuccess(&buf, ModeCompact, env, data, "v0.1.0"); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, hasID := out["command_id"]; hasID {
		t.Fatal("compact mode must not include command_id")
	}
	if _, hasGenAt := out["generated_at"]; hasGenAt {
		t.Fatal("compact mode must not include generated_at")
	}
	if out["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", out["status"])
	}
}

func TestRenderSuccess_Text_scalar(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "version")
	if err := RenderSuccess(&buf, ModeText, env, nil, "golink v0.1.0"); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	if !strings.Contains(buf.String(), "golink v0.1.0") {
		t.Fatalf("expected text output, got %q", buf.String())
	}
}

func TestRenderSuccess_JSONL_tabular(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "post list")
	td := stubTabular{
		headers: []string{"URN", "TEXT"},
		rows: [][]string{
			{"urn:li:share:1", "hello"},
			{"urn:li:share:2", "world"},
		},
	}
	if err := RenderSuccess(&buf, ModeJSONL, env, td, ""); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %s", len(lines), buf.String())
	}
	var row map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("unmarshal row 0: %v", err)
	}
	if row["urn"] != "urn:li:share:1" {
		t.Fatalf("expected urn:li:share:1, got %q", row["urn"])
	}
}

func TestRenderSuccess_JSONL_scalar(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "version")
	data := VersionData{Version: "0.1.0"}
	if err := RenderSuccess(&buf, ModeJSONL, env, data, ""); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line for scalar, got %d", len(lines))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", out["status"])
	}
}

func TestRenderSuccess_Table_tabular(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "post list")
	td := stubTabular{
		headers: []string{"URN", "TEXT"},
		rows: [][]string{
			{"urn:li:share:1", "hello"},
		},
	}
	if err := RenderSuccess(&buf, ModeTable, env, td, ""); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "URN") || !strings.Contains(out, "TEXT") {
		t.Fatalf("expected headers in table output, got %q", out)
	}
	if !strings.Contains(out, "urn:li:share:1") {
		t.Fatalf("expected row data in table output, got %q", out)
	}
}

func TestRenderSuccess_Table_scalar_fallback(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusOK, "version")
	if err := RenderSuccess(&buf, ModeTable, env, VersionData{Version: "0.1.0"}, "golink v0.1.0"); err != nil {
		t.Fatalf("RenderSuccess: %v", err)
	}
	if !strings.Contains(buf.String(), "golink v0.1.0") {
		t.Fatalf("expected text fallback for scalar table, got %q", buf.String())
	}
}

func TestRenderError_Compact(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusError, "auth status")
	if err := RenderError(&buf, ModeCompact, env, "no active session", "UNAUTHORIZED", "Token expired"); err != nil {
		t.Fatalf("RenderError: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["status"] != "error" {
		t.Fatalf("expected status error, got %v", out["status"])
	}
	if out["error"] != "no active session" {
		t.Fatalf("expected error message, got %v", out["error"])
	}
	if _, hasID := out["command_id"]; hasID {
		t.Fatal("compact error must not include command_id")
	}
}

func TestRenderError_Text(t *testing.T) {
	var buf bytes.Buffer
	env := fixtureEnvelope(StatusError, "auth status")
	if err := RenderError(&buf, ModeText, env, "no active session", "UNAUTHORIZED", "Token expired or invalid"); err != nil {
		t.Fatalf("RenderError: %v", err)
	}
	if !strings.Contains(buf.String(), "Token expired or invalid") {
		t.Fatalf("expected text in output, got %q", buf.String())
	}
}

func TestTruncateCell(t *testing.T) {
	short := "hello"
	if got := truncateCell(short); got != short {
		t.Fatalf("short cell should not be truncated: %q", got)
	}
	long := strings.Repeat("x", 70)
	got := truncateCell(long)
	if len(got) != maxCellWidth+3 {
		t.Fatalf("expected length %d, got %d", maxCellWidth+3, len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatal("truncated cell should end with ...")
	}
}

// TabularData implementation tests for the list data types.

func TestPostListData_TabularData_empty(t *testing.T) {
	d := PostListData{}
	if len(d.Rows()) != 0 {
		t.Fatal("empty PostListData should return empty rows")
	}
	if len(d.Headers()) != 6 {
		t.Fatalf("expected 6 headers, got %d", len(d.Headers()))
	}
}

func TestPostListData_TabularData_populated(t *testing.T) {
	d := PostListData{
		Items: []PostListItem{
			{
				PostSummary: PostSummary{
					ID:         "urn:li:share:1",
					CreatedAt:  fixedTime,
					Text:       "hello world",
					Visibility: VisibilityPublic,
				},
				LikeCount:    3,
				CommentCount: 1,
			},
		},
	}
	rows := d.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row[0] != "urn:li:share:1" {
		t.Fatalf("expected URN urn:li:share:1, got %q", row[0])
	}
	if row[1] != "PUBLIC" {
		t.Fatalf("expected visibility PUBLIC, got %q", row[1])
	}
	if row[3] != "3" {
		t.Fatalf("expected likes 3, got %q", row[3])
	}
	if row[4] != "1" {
		t.Fatalf("expected comments 1, got %q", row[4])
	}
	if row[5] != "hello world" {
		t.Fatalf("expected text, got %q", row[5])
	}
}

func TestCommentListData_TabularData(t *testing.T) {
	d := CommentListData{
		Items: []CommentData{
			{
				ID:        "urn:li:comment:1",
				Author:    "urn:li:person:abc",
				Text:      "great post",
				CreatedAt: fixedTime,
			},
		},
	}
	if len(d.Headers()) != 4 {
		t.Fatalf("expected 4 headers, got %d", len(d.Headers()))
	}
	rows := d.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "urn:li:comment:1" {
		t.Fatalf("expected ID, got %q", rows[0][0])
	}
}

func TestReactionListData_TabularData(t *testing.T) {
	d := ReactionListData{
		Items: []ReactionData{
			{
				Actor: "urn:li:person:abc",
				Type:  ReactionLike,
				At:    fixedTime,
			},
		},
	}
	if len(d.Headers()) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(d.Headers()))
	}
	rows := d.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][1] != "LIKE" {
		t.Fatalf("expected type LIKE, got %q", rows[0][1])
	}
}

func TestSearchPeopleData_TabularData(t *testing.T) {
	d := SearchPeopleData{
		People: []Person{
			{
				URN:      "urn:li:person:def",
				FullName: "Taylor Eng",
				Headline: "SWE",
				Location: "SG",
			},
		},
	}
	if len(d.Headers()) != 4 {
		t.Fatalf("expected 4 headers, got %d", len(d.Headers()))
	}
	rows := d.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][1] != "Taylor Eng" {
		t.Fatalf("expected name, got %q", rows[0][1])
	}
}
