# Build stage
FROM golang:1.22 AS builder
WORKDIR /src
RUN mkdir -p build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o ./build ./...

# Final image (distroless)
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /src/build/pocketid-registration-bot /pocketid-registration-bot

ENTRYPOINT ["/pocketid-registration-bot"]
