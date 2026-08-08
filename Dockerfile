# Build stage
FROM golang:1.22 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /out/pocketid-registration-bot ./...

# Final image (distroless)
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/pocketid-registration-bot /app/pocketid-registration-bot

ENTRYPOINT ["/app/pocketid-registration-bot"]
