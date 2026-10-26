# otlp-ws-bridge

Small front for a standard OTLP HTTP collector. It accepts OTLP HTTP and one JSON export per WebSocket frame, then forwards each signal to the collector.

| Mode | URL | Body |
|------|-----|------|
| OTLP HTTP | `POST /v1/{traces,logs,metrics}` | OTLP JSON or protobuf, proxied as-is |
| WebSocket | `GET /v1` + Upgrade | One OTLP **JSON** export per text or binary frame |

There is no official OTLP-over-WebSocket transport. Frames are demuxed by top-level JSON keys:

| Key | Forwarded to |
|-----|----------------|
| `resourceSpans` | `POST {upstream}/v1/traces` |
| `resourceLogs` | `POST {upstream}/v1/logs` |
| `resourceMetrics` | `POST {upstream}/v1/metrics` |

Each frame gets an ack: `{"ok":true}` or `{"ok":false,"error":"..."}`.

```bash
go test ./...
go run . -listen :4319 -upstream http://127.0.0.1:4318
```

Flags and environment: `-listen` / `LISTEN` (default `:4319`), `-upstream` / `OTLP_UPSTREAM` (default `http://127.0.0.1:4318`).

```js
const ws = new WebSocket("ws://127.0.0.1:4319/v1");
ws.binaryType = "arraybuffer";
ws.onopen = () => {
  ws.send(JSON.stringify({ resourceSpans: [] }));
};
ws.onmessage = (ev) => console.log(JSON.parse(ev.data));
```
