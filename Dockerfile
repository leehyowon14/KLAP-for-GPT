FROM node:24-bookworm-slim AS web
WORKDIR /source/web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build && npm test

FROM golang:1.26.4 AS go
WORKDIR /source
COPY go.mod go.sum ./
COPY core/go.mod core/go.sum ./core/
RUN go mod download
COPY core/ ./core/
COPY internal/ ./internal/
COPY cmd/ ./cmd/
RUN go test -timeout 90s ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /output/klap-gpt ./cmd/klap-gpt

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata curl && rm -rf /var/lib/apt/lists/* && useradd --uid 10001 --create-home klap && mkdir /data && chown klap:klap /data
WORKDIR /app
COPY --from=go /output/klap-gpt /app/klap-gpt
COPY --from=web /source/web/dist/index.html /app/ui/index.html
USER 10001:10001
ENV KLAP_LISTEN=0.0.0.0:3030 KLAP_DATA_DIR=/data KLAP_UI_FILE=/app/ui/index.html KLAP_MODE=server TZ=Asia/Seoul
EXPOSE 3030
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD curl --fail --silent http://127.0.0.1:3030/health || exit 1
ENTRYPOINT ["/app/klap-gpt"]
