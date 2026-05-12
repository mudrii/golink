package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestFormatHelpers(t *testing.T) {
	meta := EnvelopeMeta{
		Status:      StatusOK,
		CommandID:   "cmd_1",
		Command:     "version",
		Transport:   "official",
		GeneratedAt: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
	}

	success := Success(meta, VersionData{Version: "1.0.0"})
	if success.Command != "version" || success.Data.Version != "1.0.0" {
		t.Fatalf("unexpected success envelope: %+v", success)
	}

	errEnv := Error(meta, ErrorCodeTransport, "transport failed", "details")
	if errEnv.Code != ErrorCodeTransport || errEnv.Error != "transport failed" {
		t.Fatalf("unexpected error envelope: %+v", errEnv)
	}

	validationMeta := meta
	validationMeta.Status = StatusValidation
	validation := ValidationError(validationMeta, "invalid", "details")
	if validation.Code != ErrorCodeValidation {
		t.Fatalf("validation code = %q", validation.Code)
	}

	base := BuildBase(meta)
	if base.CommandID != "cmd_1" || base.Command != "version" {
		t.Fatalf("unexpected base envelope: %+v", base)
	}

	extractedEnv, msg, code, ok := ExtractErrorEnvelope(errEnv)
	if !ok || extractedEnv.Command != "version" || msg != "transport failed" || code != string(ErrorCodeTransport) {
		t.Fatalf("unexpected extracted error envelope: env=%+v msg=%q code=%q ok=%v", extractedEnv, msg, code, ok)
	}
	if _, _, _, ok := ExtractErrorEnvelope(struct{}{}); ok {
		t.Fatal("unexpected successful extraction for unknown payload")
	}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, validation); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"status":"validation_error"`) {
		t.Fatalf("unexpected encoded json: %s", buf.String())
	}
}

func TestTabularRowsCoverage(t *testing.T) {
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	scheduleRows := ScheduleListData{
		Items: []ScheduledPostItem{{
			CommandID:   "cmd_1",
			State:       ScheduleStatePending,
			ScheduledAt: now,
			Request:     ScheduleRequest{Text: "hello"},
		}},
	}
	if diff := cmp.Diff([]string{"COMMAND_ID", "STATE", "SCHEDULED_AT", "TEXT"}, scheduleRows.Headers()); diff != "" {
		t.Fatalf("schedule headers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][]string{{"cmd_1", "pending", "2026-04-17T12:00:00Z", "hello"}}, scheduleRows.Rows()); diff != "" {
		t.Fatalf("schedule rows mismatch (-want +got):\n%s", diff)
	}

	doctorRows := DoctorData{
		Features: []DoctorFeature{{Command: "post create", Status: "supported", Reason: ""}},
	}
	if diff := cmp.Diff([]string{"COMMAND", "STATUS", "REASON"}, doctorRows.Headers()); diff != "" {
		t.Fatalf("doctor headers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][]string{{"post create", "supported", ""}}, doctorRows.Rows()); diff != "" {
		t.Fatalf("doctor rows mismatch (-want +got):\n%s", diff)
	}

	socialRows := SocialMetadataData{
		Items: []SocialMetadataItem{{
			PostURN:       "urn:li:share:123456789012345",
			CommentCount:  2,
			LikeCount:     3,
			ReactionCount: 4,
			CommentsState: "ENABLED",
		}},
	}
	if diff := cmp.Diff([]string{"URN", "COMMENTS", "LIKES", "REACTIONS", "STATE"}, socialRows.Headers()); diff != "" {
		t.Fatalf("social headers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][]string{{"...456789012345", "2", "3", "4", "ENABLED"}}, socialRows.Rows()); diff != "" {
		t.Fatalf("social rows mismatch (-want +got):\n%s", diff)
	}

	searchRows := SearchPeopleData{
		People: []Person{{URN: "urn:li:person:1", FullName: "Ada", Headline: "Math", Location: "London"}},
	}
	if diff := cmp.Diff([]string{"URN", "NAME", "HEADLINE", "LOCATION"}, searchRows.Headers()); diff != "" {
		t.Fatalf("search headers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][]string{{"urn:li:person:1", "Ada", "Math", "London"}}, searchRows.Rows()); diff != "" {
		t.Fatalf("search rows mismatch (-want +got):\n%s", diff)
	}

	orgRows := OrgListData{
		Items: []OrgListItem{{URN: "urn:li:organization:1", Role: "ADMINISTRATOR", State: "APPROVED", Name: "Acme"}},
	}
	if diff := cmp.Diff([]string{"URN", "ROLE", "STATE", "NAME"}, orgRows.Headers()); diff != "" {
		t.Fatalf("org headers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][]string{{"urn:li:organization:1", "ADMINISTRATOR", "APPROVED", "Acme"}}, orgRows.Rows()); diff != "" {
		t.Fatalf("org rows mismatch (-want +got):\n%s", diff)
	}
}

func TestBaseUsesCurrentTimeWhenZero(t *testing.T) {
	b := base(EnvelopeMeta{Status: StatusOK, Command: "version", Transport: "official"})
	if b.GeneratedAt.IsZero() {
		t.Fatal("expected generated_at to be populated")
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	payload := compactSuccess{
		Status:  StatusOK,
		Command: "version",
		Data:    VersionData{Version: "1.0.0"},
	}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var decoded compactSuccess
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Command != "version" {
		t.Fatalf("command = %q", decoded.Command)
	}
}
