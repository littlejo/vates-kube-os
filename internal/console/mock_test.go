package console

import (
	"testing"

	"github.com/vatesfr/vates-kube-os/internal/dashboard"
)

// The mock is a feed, and a feed slides by one: the second reading must be the
// first with one event added at the bottom and one gone from the top. If a
// refresh re-stamps what it already showed, the display's anchoring has nothing
// to hold on to and a reader who scrolled up watches the text change under them
// -- which is exactly the fault this is here to catch.
func TestMockSlidesByOne(t *testing.T) {
	first := mockSnapshot()
	second := mockSnapshot()

	if len(first.Events) == 0 || len(second.Events) == 0 {
		t.Fatal("the mock produced no events")
	}
	if len(first.Events) != len(second.Events) {
		t.Fatalf("window changed size: %d then %d", len(first.Events), len(second.Events))
	}
	for i := 1; i < len(first.Events); i++ {
		if dashboard.EventKey(first.Events[i]) != dashboard.EventKey(second.Events[i-1]) {
			t.Fatalf("event %d is not the one that was at %d: the mock re-stamps what it already showed", i, i-1)
		}
	}
	// And the newest must be genuinely new.
	if dashboard.EventKey(first.Events[len(first.Events)-1]) == dashboard.EventKey(second.Events[len(second.Events)-1]) {
		t.Fatal("the newest event did not change: the feed is a still life")
	}
}
