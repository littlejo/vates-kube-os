package kubeapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The console polls these reads every couple of seconds on EVERY node, so a read
// with no resourceVersion is a QUORUM read: the API server forwards it to etcd
// and waits for a linearizable answer. A fleet of consoles then becomes a steady
// stream of etcd round-trips, which starves a control plane whose etcd is on a
// slow disk. Asserting resourceVersion=0 is asserting the console leaves etcd
// alone.
func TestReadsAskForTheWatchCache(t *testing.T) {
	tests := []struct {
		name string
		body string
		call func(*Client) error
	}{
		{
			name: "node",
			body: `{"metadata":{"name":"vates-0"}}`,
			call: func(c *Client) error { _, err := c.Node("vates-0"); return err },
		},
		{
			name: "pods",
			body: `{"items":[]}`,
			call: func(c *Client) error { _, err := c.PodsOn("vates-0"); return err },
		},
		{
			name: "events",
			body: `{"items":[]}`,
			call: func(c *Client) error { _, err := c.Events(400); return err },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			c := &Client{Server: srv.URL, http: srv.Client()}
			if err := tt.call(c); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if got.Get("resourceVersion") != "0" {
				t.Errorf("resourceVersion = %q, want 0; without it this poll is a quorum read straight to etcd (%s)",
					got.Get("resourceVersion"), got.Encode())
			}
		})
	}
}
