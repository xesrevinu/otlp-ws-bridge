package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHTTPPassthrough(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var gotPath string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotPath = r.URL.Path
		gotBody = string(body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
	}))
	t.Cleanup(upstream.Close)

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge(u, nil)
	srv := httptest.NewServer(bridge.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/v1/traces", "application/json", strings.NewReader(`{"resourceSpans":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/v1/traces" {
		t.Fatalf("path %q", gotPath)
	}
	if gotBody != `{"resourceSpans":[]}` {
		t.Fatalf("body %q", gotBody)
	}
}

func TestWebSocketDemux(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
	}))
	t.Cleanup(upstream.Close)

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge(u, nil)
	srv := httptest.NewServer(bridge.Handler())
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, srv.URL+"/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	frames := []string{
		`{"resourceSpans":[]}`,
		`{"resourceLogs":[]}`,
		`{"resourceMetrics":[]}`,
		`{"nope":true}`,
	}
	for _, frame := range frames {
		if err := conn.Write(ctx, websocket.MessageBinary, []byte(frame)); err != nil {
			t.Fatal(err)
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ack wsAck
		if err := json.Unmarshal(data, &ack); err != nil {
			t.Fatal(err)
		}
		if frame == `{"nope":true}` {
			if ack.OK {
				t.Fatalf("expected error ack for bad frame")
			}
			continue
		}
		if !ack.OK {
			t.Fatalf("ack failed for %s: %s", frame, ack.Error)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"/v1/traces", "/v1/logs", "/v1/metrics"}
	if len(paths) != len(want) {
		t.Fatalf("paths=%v want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths=%v want %v", paths, want)
		}
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("http://127.0.0.1:9")
	srv := httptest.NewServer(NewBridge(u, nil).Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

