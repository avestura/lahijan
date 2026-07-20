ARG LAHIJAN_IMAGE_TAG="dev"

# Build the Go binary. Pinned to Go 1.25 to match go.mod's go directive.
FROM golang:1.25-alpine AS builder
WORKDIR /tmp/app
ARG LAHIJAN_IMAGE_TAG

COPY ./go.mod ./go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod/ go mod download -x

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache \
    CGO_ENABLED=0 GOOS=linux go build -C . -ldflags "-X 'github.com/avestura/lahijan/internal/app/lahijan/version.LahijanVersion=$LAHIJAN_IMAGE_TAG'" -o dist/build ./cmd/lahijan/main.go


# Runtime image. Use the official alpine tag (alpine:3.22), not the bare
# "alpine3.22" form which Docker would treat as an image named alpine3.22
# from Docker Hub (it does not exist).
FROM alpine:3.22

COPY --from=builder /tmp/app/dist/build /etc/lahijan/server

WORKDIR /etc/lahijan/
CMD ["./server"]
