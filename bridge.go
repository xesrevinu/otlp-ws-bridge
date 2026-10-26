package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultMaxFrameBytes = 4 << 20 // 4 MiB
	defaultIdleTimeout   = 5 * time.Minute
	defaultForwardTimeout = 30 * time.Second
)

// Bridge terminates OTLP HTTP and a JSON-over-WebSocket ingest path, then
// forwards to a standard OTLP HTTP collector.
type Bridge struct {
	Upstream        *url.URL
	Log             *slog.Logger
	MaxFrameBytes   int64
	IdleTimeout     time.Duration
	ForwardTimeout  time.Duration
	HTTPClient      *http.Client
	proxy           *httputil.ReverseProxy
}

func NewBridge(upstream *url.URL, log *slog.Logger) *Bridge {
	if log == nil {
		log = slog.Default()
	}
	b := &Bridge{
		Upstream:       upstream,
		Log:            log,
		MaxFrameBytes:  defaultMaxFrameBytes,
		IdleTimeout:    defaultIdleTimeout,
		ForwardTimeout: defaultForwardTimeout,
		HTTPClient: &http.Client{
			Timeout: defaultForwardTimeout,
		},
	}
	b.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(b.Upstream)
			r.Out.Host = b.Upstream.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			b.Log.Error("http proxy error", "path", r.URL.Path, "error", err)
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
		},
	}
	return b
}

func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", b.handleHealthz)
	mux.HandleFunc("GET /v1", b.handleV1)
	mux.HandleFunc("POST /v1/traces", b.handleOTLPHTTP)
	mux.HandleFunc("POST /v1/logs", b.handleOTLPHTTP)
	mux.HandleFunc("POST /v1/metrics", b.handleOTLPHTTP)
	return mux
}

func (b *Bridge) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (b *Bridge) handleOTLPHTTP(w http.ResponseWriter, r *http.Request) {
	b.proxy.ServeHTTP(w, r)
}

func (b *Bridge) handleV1(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		b.handleWS(w, r)
		return
	}
	http.Error(w, "use POST /v1/{traces,logs,metrics} or WebSocket Upgrade on /v1", http.StatusMethodNotAllowed)
}

type wsAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (b *Bridge) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The caller terminates TLS. This server does not check Origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		b.Log.Warn("websocket accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	conn.SetReadLimit(b.MaxFrameBytes)
	ctx := r.Context()

	for {
		readCtx, cancel := context.WithTimeout(ctx, b.IdleTimeout)
		msgType, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return
		}
		if msgType != websocket.MessageText && msgType != websocket.MessageBinary {
			_ = b.writeAck(ctx, conn, wsAck{OK: false, Error: "unsupported websocket message type"})
			continue
		}

		signal, err := DetectSignal(data)
		if err != nil {
			_ = b.writeAck(ctx, conn, wsAck{OK: false, Error: err.Error()})
			continue
		}

		if err := b.forwardOTLP(ctx, signal, data); err != nil {
			b.Log.Warn("ws forward failed", "signal", signal, "error", err)
			_ = b.writeAck(ctx, conn, wsAck{OK: false, Error: err.Error()})
			continue
		}
		_ = b.writeAck(ctx, conn, wsAck{OK: true})
	}
}

func (b *Bridge) writeAck(ctx context.Context, conn *websocket.Conn, ack wsAck) error {
	payload, err := json.Marshal(ack)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

func (b *Bridge) forwardOTLP(ctx context.Context, signal Signal, body []byte) error {
	path := signal.UpstreamPath()
	if path == "" {
		return fmt.Errorf("unknown signal %q", signal)
	}
	u := b.Upstream.JoinPath(path)
	reqCtx, cancel := context.WithTimeout(ctx, b.ForwardTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := b.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("upstream %s: %s", path, res.Status)
	}
	return nil
}
