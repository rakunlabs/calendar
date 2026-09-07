# HTTP Server

`NewServer(ctx context.Context, svc port.CalendarService) (*ada.Server, error)`
constructs the Ada router without opening a listener. The caller owns lifecycle:

```go
s, err := server.NewServer(ctx, svc)
if err != nil {
    return err
}
return s.StartWithContext(ctx, ":8080")
```

Ada v0.5.1 exposes
`StartWithContext(ctx context.Context, addr string, opts ...ada.OptionStart) error`
and `Stop() error`. Start blocks, cancellation initiates graceful shutdown, and
normal server closure returns nil. The default shutdown timeout is 10 seconds.
The construction context does not start or stop the server.

Lifecycle caveat in v0.5.1: context cancellation calls `Stop` through
`context.AfterFunc`. `Serve` can return when the listener closes before that
callback finishes draining requests. If caller cleanup must wait for the full
drain, coordinate `Start` and `Stop` explicitly and wait for `Stop` to finish.

The middleware order is recover, server, CORS, request ID, access log, telemetry,
following <https://rakunlabs.github.io/ada/guide/microservice.html>.
Swagger UI remains at `/calendar/swagger/index.html`, with the spec at
`/calendar/swagger/doc.json`, using Ada's Swagger handler. API routes remain under
`/calendar/v1`. Run `make docs` to refresh the checked-in document from
handler annotations.

## Preserved Contracts

- Query uses `github.com/rakunlabs/query v0.5.0`, which fixes encoded filter
  values such as `name=R%26D`. The HTTP adapter aliases `limit`, `offset`, `sort`,
  and `fields` to `_limit`, `_offset`, `_sort`, and `_fields`, accepting both
  forms. An explicit underscore-prefixed target takes priority over its alias
  regardless of parameter order. Endpoint validation applies to both forms.

- Responses retain `message`, `meta`, and `payload`, including the existing JSON
  omission rules and empty-body HTTP 200 responses for event deletion.
- String HTTP errors use `message.text`; error-valued HTTP errors use generic
  `Internal Server Error` text plus `message.error`, including binding errors
  with HTTP 400. Bare service errors and handler panics remain generic HTTP 500.
- `X-User` is the unchanged source of `updated_by`, overriding request-body
  identity. Missing headers yield an empty identity.
- Event/relation POST bodies use Ada's `WithJSONSingleAsSlice(true)` to accept
  objects, arrays, and `null`. They require `Content-Type: application/json` and
  reject trailing data. Event PUT retains single-value JSON
  decoder semantics, including acceptance of trailing data and `null`.
- Ada's recovery middleware uses `WithErrorHandler` to preserve the JSON error
  envelope for handler and middleware panics without exposing panic details.
  After a response is committed, it aborts handling instead of appending a
  second response body. Individual routes need no recovery wrappers.

References: <https://rakunlabs.github.io/ada/guide/handler/swagger.html> and the
installed Ada v0.5.1 and worldline-go/rest v0.1.5 source.

The new binder and configurable recovery currently require the local `../ada`
checkout via temporary `go.mod` replace directives for Ada and its separate
`middleware/recover` module. Remove the replacements and update the respective
requirements when releases are published; until then builds require that sibling
checkout.
