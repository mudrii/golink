package cmd

import (
	"testing"
	"time"

	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/schedule"
)

func TestCachedPostURNExtractsFromPostCreateData(t *testing.T) {
	posted := `{"id":"urn:li:share:123"}`
	id := cachedPostURN(idempotency.Entry{Result: []byte(posted)})
	if id != "urn:li:share:123" {
		t.Fatalf("cachedPostURN = %q", id)
	}
}

func TestCachedPostURNExtractsFromLegacyShape(t *testing.T) {
	raw := `{"id":"urn:li:share:legacy","visibility":{}}`
	id := cachedPostURN(idempotency.Entry{Result: []byte(raw)})
	if id != "urn:li:share:legacy" {
		t.Fatalf("cachedPostURN = %q", id)
	}
}

func TestCachedPostURNEmptyForBadPayload(t *testing.T) {
	if id := cachedPostURN(idempotency.Entry{Result: []byte(`{`)}); id != "" {
		t.Fatalf("cachedPostURN = %q", id)
	}
	if id := cachedPostURN(idempotency.Entry{}); id != "" {
		t.Fatalf("cachedPostURN = %q", id)
	}
}

func TestEntryToItemAndDataConvertLastRunAt(t *testing.T) {
	last := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	entry := schedule.Entry{
		CommandID:   "cmd-1",
		State:       schedule.StateFailed,
		ScheduledAt: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		LastRunAt:   &last,
		LastError:   "boom",
		RetryCount:  2,
		Profile:     "default",
		Transport:   "official",
		Request: schedule.Request{
			Text:       "hello",
			Visibility: "PUBLIC",
			ImagePath:  "/tmp/a.png",
			ImageAlt:   "a",
		},
	}

	item := entryToItem(entry)
	if item.CommandID != "cmd-1" {
		t.Fatalf("item.CommandID = %q", item.CommandID)
	}
	if item.State != output.ScheduleStateFailed {
		t.Fatalf("item.State = %q", item.State)
	}
	if item.RetryCount != 2 {
		t.Fatalf("item.RetryCount = %d", item.RetryCount)
	}
	if item.LastRunAt == nil || !item.LastRunAt.Equal(last) {
		t.Fatalf("item.LastRunAt = %#v", item.LastRunAt)
	}

	data := entryToData(entry)
	if data.CommandID != "cmd-1" {
		t.Fatalf("data.CommandID = %q", data.CommandID)
	}
	if data.State != output.ScheduleStateFailed {
		t.Fatalf("data.State = %q", data.State)
	}
	if data.LastRunAt == nil || !data.LastRunAt.Equal(last) {
		t.Fatalf("data.LastRunAt = %#v", data.LastRunAt)
	}
	if data.Request != (output.ScheduleRequest{
		Text:       "hello",
		Visibility: "PUBLIC",
		ImagePath:  "/tmp/a.png",
		ImageAlt:   "a",
	}) {
		t.Fatalf("data.Request = %#v", data.Request)
	}
}

func TestEntryToItemAndDataWithoutLastRunAt(t *testing.T) {
	entry := schedule.Entry{
		CommandID: "cmd-2",
	}

	if got := entryToItem(entry); got.LastRunAt != nil {
		t.Fatalf("item.LastRunAt should be nil, got %#v", got.LastRunAt)
	}
	if got := entryToData(entry); got.LastRunAt != nil {
		t.Fatalf("data.LastRunAt should be nil, got %#v", got.LastRunAt)
	}
}
