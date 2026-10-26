package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Signal is an OTLP signal kind inferred from a JSON export body.
type Signal string

const (
	SignalTraces  Signal = "traces"
	SignalLogs    Signal = "logs"
	SignalMetrics Signal = "metrics"
)

// UpstreamPath returns the OTLP HTTP path for a signal.
func (s Signal) UpstreamPath() string {
	switch s {
	case SignalTraces:
		return "/v1/traces"
	case SignalLogs:
		return "/v1/logs"
	case SignalMetrics:
		return "/v1/metrics"
	default:
		return ""
	}
}

// DetectSignal inspects a standard OTLP JSON Export*ServiceRequest and
// returns which pipeline it belongs to. Exactly one of resourceSpans,
// resourceLogs, or resourceMetrics must be present.
func DetectSignal(body []byte) (Signal, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("empty frame")
	}
	if trimmed[0] != '{' {
		return "", fmt.Errorf("frame must be a JSON object")
	}

	var keys struct {
		ResourceSpans   json.RawMessage `json:"resourceSpans"`
		ResourceLogs    json.RawMessage `json:"resourceLogs"`
		ResourceMetrics json.RawMessage `json:"resourceMetrics"`
	}
	if err := json.Unmarshal(trimmed, &keys); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	hasSpans := keys.ResourceSpans != nil
	hasLogs := keys.ResourceLogs != nil
	hasMetrics := keys.ResourceMetrics != nil
	n := 0
	if hasSpans {
		n++
	}
	if hasLogs {
		n++
	}
	if hasMetrics {
		n++
	}
	switch {
	case n == 0:
		return "", fmt.Errorf("missing resourceSpans, resourceLogs, or resourceMetrics")
	case n > 1:
		return "", fmt.Errorf("frame must contain exactly one OTLP signal key")
	case hasSpans:
		return SignalTraces, nil
	case hasLogs:
		return SignalLogs, nil
	default:
		return SignalMetrics, nil
	}
}
