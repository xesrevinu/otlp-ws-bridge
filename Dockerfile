FROM golang:1.26.5-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/otlp-ws-bridge .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/otlp-ws-bridge /usr/local/bin/otlp-ws-bridge
EXPOSE 4319
ENTRYPOINT ["/usr/local/bin/otlp-ws-bridge"]
