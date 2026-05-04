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
  working, and `OriginalError()` exposes it for logging.
- `sql.ErrNoRows` maps to `codes.NotFound` and `context.Canceled` maps to
  `codes.Canceled` automatically. Anything else without a code falls back to
  `codes.Internal`.

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
// Logs/Sentry can still inspect the full chain via OriginalError().
return nil, grpcerr.Convert(domain)
```

### Accessing the original error in interceptors

```go
if oe, ok := err.(interface{ OriginalError() error }); ok {
    log.Error("rpc failed", "err", oe.OriginalError())
}
```

## API

- `New(msg string, code codes.Code, attrs ...serr.Attributed) ConvertibleError`
- `Wrap(msg string, err error, code codes.Code, attrs ...serr.Attributed) ConvertibleError`
- `Convert(err error) error` — produces an error whose `GRPCStatus()` carries
  the resolved code and message; `Unwrap()` returns the original chain.
- `Convertible` — interface for any error that can report its own
  `GRPCErrorCode()`. Wrapped errors (`Unwrap() error` and `Unwrap() []error`)
  are traversed automatically.

## Code resolution

`Convert` resolves the gRPC code in this order:

1. `Convertible.GRPCErrorCode()` on the error or anywhere in its `Unwrap`
   chain (single or joined).
2. `errors.Is(err, sql.ErrNoRows)` → `codes.NotFound`.
3. `errors.Is(err, context.Canceled)` → `codes.Canceled`.
4. Fallback → `codes.Internal`.

Joined errors must not provide more than one distinct gRPC code; doing so
panics.
