# syntax=docker/dockerfile:1

# --- build ---
FROM golang:1.24-alpine AS build
WORKDIR /src

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY=https://proxy.golang.org,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

# --- runtime ---
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/gateway /app/gateway
COPY configs/config.example.yaml /app/configs/config.yaml

EXPOSE 8002

USER nonroot:nonroot

ENTRYPOINT ["/app/gateway"]
CMD ["-config", "/app/configs/config.yaml"]
