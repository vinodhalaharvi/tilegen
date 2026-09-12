# tilegen as a service: a spec in, a scaffolded project out.
#
# The binary executes nothing - no git, no sqlc, no build - so the image
# needs no toolchain and no shell. It runs as a non-root user on a
# distroless base, and Cloud Run's PORT is picked up automatically.

FROM golang:1.26-alpine AS build
WORKDIR /src
# Dependencies first, so a spec-only change does not refetch them.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static, stripped, reproducible.
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -ldflags="-s -w" -o /tilegen .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /tilegen /tilegen
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tilegen", "serve"]
