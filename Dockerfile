ARG LAHIJAN_IMAGE_TAG="dev"

FROM golang:1.23-alpine AS builder
WORKDIR /tmp/app
ARG LAHIJAN_IMAGE_TAG

COPY ./go.mod ./go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod/ go mod download -x

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache \
    CGO_ENABLED=0 GOOS=linux go build -C . -ldflags "-X 'github.com/avestura/lahijan/internal/app/lahijan/version.LahijanVersion=$LAHIJAN_IMAGE_TAG'" -o dist/build ./cmd/lahijan/main.go


FROM alpine3.22

COPY --from=builder /tmp/app/dist/build /etc/lahijan/server

WORKDIR /etc/lahijan/
CMD ["./server"]
