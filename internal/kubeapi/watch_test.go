package kubeapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A watch is the fast path, so what it does with a stream matters more than what
// it does with a list: it must hand over each event as it arrives, ignore the
// bookmarks the API inserts to keep the connection alive, and END cleanly when
// the server closes -- an error at the end of a healthy stream would make the
// console log a fault every five minutes for ever.
func TestWatchEventsStreamsAndEnds(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"ADDED","object":{"reason":"Started","message":"Started container kube-proxy"}}`,
		`{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"4711"}}}`,
		`{"type":"MODIFIED","object":{"reason":"Pulled","message":"Successfully pulled image"}}`,
	}, "\n") + "\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			t.Errorf("the request is not a watch: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("allowWatchBookmarks") != "true" {
			t.Error("bookmarks are not allowed: the API will close the stream on its own schedule")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, stream)
	}))
	defer srv.Close()

	c := &Client{Server: srv.URL, http: srv.Client()}
	var got []string
	err := c.WatchEvents(context.Background(), func(e Event) { got = append(got, e.Reason) })
	if err != nil {
		t.Fatalf("a stream that ended normally was reported as an error: %v", err)
	}
	if len(got) != 2 || got[0] != "Started" || got[1] != "Pulled" {
		t.Fatalf("the events handed over are %v; want [Started Pulled]", got)
	}
}

// A refused watch is an ordinary error, not a panic and not a silence: the
// system:node credential may well not carry watch on cluster-wide events, and
// the console has to be able to say so and keep its polling.
func TestWatchEventsReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"events is forbidden: User \"system:node:vates-cp-1\" cannot watch"}`)
	}))
	defer srv.Close()

	c := &Client{Server: srv.URL, http: srv.Client()}
	err := c.WatchEvents(context.Background(), func(Event) {})
	if err == nil {
		t.Fatal("a forbidden watch was reported as success")
	}
	// The API's own sentence is the useful part; the status code alone would
	// send an operator looking in the wrong place.
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("the refusal lost the API's message: %v", err)
	}
}
