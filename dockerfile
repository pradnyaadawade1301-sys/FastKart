# ── Stage 1: Build ────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder
WORKDIR /app

# Dependencies pehle copy karo (cache ke liye)
COPY go.mod go.sum ./
RUN go mod download

# Source copy karo aur build karo
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o fastkart ./cmd/main.go/

# ── Stage 2: Run ─────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Kolkata

WORKDIR /app

COPY --from=builder /app/fastkart .
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./fastkart"]