# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
# Dependencies are vendored (./vendor), so the build needs no network access.
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/server /usr/local/bin/server
ENV PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/health" >/dev/null || exit 1
ENTRYPOINT ["server"]
