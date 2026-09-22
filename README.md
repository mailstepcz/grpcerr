# grpcerr

Go support for gRPC error codes. Lets domain errors carry a `codes.Code` and an
optional user-facing message, then converts them into proper gRPC status errors
at the service boundary while keeping the original error chain available for
logging and observability.

## Install

```sh
go get github.com/mailstepcz/grpcerr
```

## Overview

- Tag errors with a gRPC status code via `New` / `Wrap`, or by implementing the
  `Convertible` interface.
- Call `Convert` once at the gRPC handler boundary to turn a domain error into a
  `*status.Status`-bearing error.
- The wire-level status message is the user-friendly message extracted via
  `serr.ExtractUserMessage` when present, otherwise `err.Error()`.
- The original (rich) error chain is preserved — `errors.Is`/`errors.As` keep
  working, and `Original` (or the `OriginalErrorer` interface) exposes it for
  logging.
- `sql.ErrNoRows` maps to `codes.NotFound` and any form of caller cancellation
  maps to `codes.Canceled` automatically. Anything else without a code falls
  back to `codes.Internal`.

## Usage

### Tagging an error with a gRPC code

```go
import (
    "errors"

    "github.com/mailstepcz/grpcerr"
    "google.golang.org/grpc/codes"
)

var ErrEntityMissing = grpcerr.New("entity missing", codes.NotFound)

func loadEntity(id string) error {
    return grpcerr.Wrap("loading entity", ErrEntityMissing, codes.NotFound)
}
```

### Converting at the gRPC boundary

```go
func (s *Server) GetEntity(ctx context.Context, req *pb.GetEntityRequest) (*pb.Entity, error) {
    e, err := s.repo.Load(ctx, req.GetId())
    if err != nil {
        return nil, grpcerr.Convert(err)
    }
    return e, nil
}
```

### Attaching a user-friendly message

`Convert` integrates with [`serr`](https://github.com/mailstepcz/serr): if any
error in the chain carries a user message via `serr.WithUserMessage`, that
message becomes the gRPC status message sent to the client. The technical chain
remains accessible for logs.

```go
domain := serr.WithUserMessage(
    grpcerr.Wrap("", errFromRepo, codes.NotFound),
    "Order not found.",
)

// Client sees status code NotFound with message "Order not found."
// Logs/Sentry can still inspect the full chain via grpcerr.Original.
return nil, grpcerr.Convert(domain)
```

### Accessing the original error in interceptors

```go
log.Error("rpc failed", "err", grpcerr.Original(err))
```

`Original` returns the rich pre-conversion chain when `err` (or anything in its
chain) implements `OriginalErrorer`; otherwise it returns `err` unchanged.

## API

- `New(msg string, code codes.Code, attrs ...serr.Attributed) ConvertibleError`
- `Wrap(msg string, err error, code codes.Code, attrs ...serr.Attributed) ConvertibleError`
- `Convert(err error) error` — produces an error whose `GRPCStatus()` carries
  the resolved code and message; `Unwrap()` returns the original chain.
- `Convertible` — interface for any error that can report its own
  `GRPCErrorCode()`. Wrapped errors (`Unwrap() error` and `Unwrap() []error`)
  are traversed automatically.
- `OriginalErrorer` — interface implemented by errors returned from `Convert`,
  exposing the original (pre-conversion) chain via `OriginalError() error`.
- `Original(err error) error` — returns `err`'s original chain if available,
  otherwise returns `err` unchanged.
- `IsCanceled(err error) bool` — reports whether `err` is, or wraps, a
  cancellation caused by the caller going away.

## Code resolution

`Convert` resolves the gRPC code in this order:

1. `Convertible.GRPCErrorCode()` on the error or anywhere in its `Unwrap`
   chain (single or joined).
2. `errors.Is(err, sql.ErrNoRows)` → `codes.NotFound`.
3. `IsCanceled(err)` → `codes.Canceled`.
4. Fallback → `codes.Internal`.

Joined errors must not provide more than one distinct gRPC code; doing so
panics.

## Detecting cancellation

A cancelled caller does not reach the service as one single error type, so
`errors.Is(err, context.Canceled)` alone misses most of the real cases.
`IsCanceled` recognises all of them:

| Boundary | Error returned | `errors.Is(err, context.Canceled)` |
| --- | --- | --- |
| outbound gRPC call | `*status.Error` with `codes.Canceled` | **false** |
| Postgres, server cancels the statement | SQLSTATE `57014` (`query_canceled`) | **false** |
| Postgres, driver aborts first | `context.Canceled` | true |
| HTTP client | `*url.Error` | true |
| AWS SDK / smithy | `*smithy.OperationError` | true |

A gRPC status is the trap: grpc-go gives `*status.Error` its own `Is` that only
matches another status, so a `codes.Canceled` status never unwraps to
`context.Canceled`.

An explicitly tagged code always wins — `Wrap("", err, codes.Internal)` stays
`Internal` even if its cause was cancelled. `IsCanceled` inspects
`Original(err)`, so it also works on an error already returned from `Convert`.

```go
// in a Sentry or logging interceptor: a cancelled caller is not a failure
if grpcerr.IsCanceled(err) {
    logger.WarnContext(ctx, "call cancelled by caller", slog.String("error", err.Error()))
    return
}
```

`context.DeadlineExceeded` is deliberately **not** treated as a cancellation — a
timeout is a real signal worth reporting.
