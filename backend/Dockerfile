# --- 1. Build stage ---
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates git tzdata && update-ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server/main.go

# --- 2. Runtime stage ---
FROM alpine:latest

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

ENV TZ=Asia/Tokyo

WORKDIR /app

COPY --from=builder --chown=app:app /app/server /app/server
COPY --from=builder --chown=app:app /app/db /app/db
COPY --from=builder --chown=app:app /app/uploads /app/uploads

USER app

EXPOSE 8080

CMD ["/app/server"]
