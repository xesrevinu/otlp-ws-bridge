package main

import "testing"

func TestDetectSignal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		body    string
		want    Signal
		wantErr bool
	}{
		{name: "traces", body: `{"resourceSpans":[]}`, want: SignalTraces},
		{name: "logs", body: `{"resourceLogs":[]}`, want: SignalLogs},
		{name: "metrics", body: `{"resourceMetrics":[]}`, want: SignalMetrics},
		{name: "whitespace", body: "  \n{\"resourceSpans\":[]}  ", want: SignalTraces},
		{name: "empty", body: ``, wantErr: true},
		{name: "array", body: `[]`, wantErr: true},
		{name: "no keys", body: `{"foo":1}`, wantErr: true},
		{name: "multi", body: `{"resourceSpans":[],"resourceLogs":[]}`, wantErr: true},
		{name: "null key still counts", body: `{"resourceSpans":null}`, want: SignalTraces},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DetectSignal([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if got.UpstreamPath() == "" {
				t.Fatalf("empty upstream path for %q", got)
			}
		})
	}
}
