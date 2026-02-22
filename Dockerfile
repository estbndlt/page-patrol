FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=arm64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/page-patrol-web ./cmd/web
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/page-patrol-worker ./cmd/worker

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S app -G app

COPY --from=builder /out/page-patrol-web /app/page-patrol-web
COPY --from=builder /out/page-patrol-worker /app/page-patrol-worker
COPY --from=builder /src/internal/db/migrations /app/internal/db/migrations
COPY --from=builder /src/web/templates /app/web/templates
COPY --from=builder /src/web/static /app/web/static

ENV APP_LISTEN_ADDR=:8080
ENV MIGRATIONS_DIR=/app/internal/db/migrations
ENV TEMPLATE_DIR=/app/web/templates
ENV STATIC_DIR=/app/web/static

USER app
EXPOSE 8080
CMD ["/app/page-patrol-web"]
