package app

import (
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

func TestNotificationEventMapping(t *testing.T) {
	cases := map[domain.RunStatus]string{
		domain.StatusSuccess:  "change",
		domain.StatusFailed:   "failure",
		domain.StatusNoChange: "success",
		domain.StatusSkipped:  "success",
	}
	for status, want := range cases {
		if got := eventFor(status); got != want {
			t.Fatalf("eventFor(%s) = %s, want %s", status, got, want)
		}
	}
}

func TestNotificationSubscriptions(t *testing.T) {
	if subscribes(nil, "change") {
		t.Fatal("an empty list must not receive change events")
	}
	if !subscribes(nil, "failure") {
		t.Fatal("an empty list must default to failures")
	}
	if !subscribes([]string{"all"}, "change") {
		t.Fatal("\"all\" must match every event")
	}
	if !subscribes([]string{"change"}, "change") {
		t.Fatal("explicit match should fire")
	}
	if subscribes([]string{"change"}, "failure") {
		t.Fatal("a change-only channel must not fire on failure")
	}
}
