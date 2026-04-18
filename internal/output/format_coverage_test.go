package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	if len(scheduleRows.Headers()) != 4 || len(scheduleRows.Rows()) != 1 {
		t.Fatalf("unexpected schedule rows")
	}

	doctorRows := DoctorData{
		Features: []DoctorFeature{{Command: "post create", Status: "supported", Reason: ""}},
	}
	if len(doctorRows.Headers()) != 3 || len(doctorRows.Rows()) != 1 {
		t.Fatalf("unexpected doctor rows")
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
	if rows := socialRows.Rows(); len(rows) != 1 || rows[0][0] == socialRows.Items[0].PostURN {
		t.Fatalf("expected shortened urn in first column, got %+v", rows)
	}

	searchRows := SearchPeopleData{
		People: []Person{{URN: "urn:li:person:1", FullName: "Ada", Headline: "Math", Location: "London"}},
	}
	if len(searchRows.Headers()) != 4 || len(searchRows.Rows()) != 1 {
		t.Fatalf("unexpected search rows")
	}

	orgRows := OrgListData{
		Items: []OrgListItem{{URN: "urn:li:organization:1", Role: "ADMINISTRATOR", State: "APPROVED", Name: "Acme"}},
	}
	if len(orgRows.Headers()) != 4 || len(orgRows.Rows()) != 1 {
		t.Fatalf("unexpected org rows")
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
