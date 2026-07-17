# `pkg/lahijan-client` — Lahijan Go client SDK

A self-contained Go client for the Lahijan public REST API, generated from
[`api/openapi.yaml`](../../api/openapi.yaml) by
[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen). This is the
SDK third-party Go programs use to talk to a Lahijan deployment.

> **Never hand-edit files here.** Regenerate with `make openapi-gen`; CI fails
> if the committed code drifts from the spec (`make openapi-verify`).

## Usage

```go
import "github.com/avestura/lahijan/pkg/lahijan-client"

client, err := lahijanclient.NewClient("https://api.lahijan.dev")
if err != nil { /* ... */ }

// Typed request/response: the WithResponses variant decodes the body for you.
resp, err := client.PingWithResponse(context.Background())
// resp.JSON200.Pong is a time.Time
```

## What's generated

- `client_gen.go` — models (`Pong`, `User`, `Error`, ...) + `Client` /
  `ClientWithResponses` + request builders, all type-checked against the spec.

The package is generated with `generate: { models: true, client: true }` so it
carries its own copy of the models (independent of the server package under
`api/gen/go`). See [`api/oapi-codegen.client.yaml`](../../api/oapi-codegen.client.yaml).
