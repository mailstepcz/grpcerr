package grpcerr

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mailstepcz/cache"
	"github.com/mailstepcz/serr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Convertible is an error convertible into a gRPC error.
type Convertible interface {
	error
	GRPCErrorCode() codes.Code
}

// ConvertibleError is an error convertible into a gRPC error.
type ConvertibleError struct {
	err  error
	code codes.Code
}

// New creates a new error convertible into a gRPC error.
func New(err string, code codes.Code, attrs ...serr.Attributed) ConvertibleError {
	return Wrap("", errors.New(err), code, attrs...)
}

// Wrap wraps an error into one convertible into a gRPC error.
func Wrap(msg string, err error, code codes.Code, attrs ...serr.Attributed) ConvertibleError {
	if len(attrs) > 0 {
		err = serr.Wrap(msg, err, attrs...)
	} else if msg != "" {
		err = fmt.Errorf("%s: %w", msg, err)
	}
	return ConvertibleError{
		err:  err,
		code: code,
	}
}

func (err ConvertibleError) Error() string {
	return err.err.Error()
}

func (err ConvertibleError) Unwrap() error {
	return err.err
}

// GRPCErrorCode is the corresponding gRPC error code.
func (err ConvertibleError) GRPCErrorCode() codes.Code {
	return err.code
}

var _ Convertible = ConvertibleError{}

var (
	codeCache cache.Cache[error, codes.Code]
)

// Convert converts the error into a gRPC error.
//
// If err (or anything in its chain) carries a serr.UserMessager user-friendly
// message, that message is used as the gRPC status message — the technical
// chain stays available via the returned error's OriginalError() for logging
// and Sentry. Without a user message, the message is err.Error() — identical
// to legacy behavior.
func Convert(err error) error {
	msg := serr.ExtractUserMessage(err)
	if len(msg) == 0 {
		msg = err.Error()
	}

	if c, ok := getGRPCCode(err); ok {
		return &convertedError{grpcErr: status.Error(c, msg), original: err}
	}

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return &convertedError{grpcErr: status.Error(codes.NotFound, msg), original: err}
	case errors.Is(err, context.Canceled):
		return &convertedError{grpcErr: status.Error(codes.Canceled, msg), original: err}
	}

	return &convertedError{grpcErr: status.Error(codes.Internal, msg), original: err}
}

// OriginalErrorer is implemented by errors returned from Convert. It exposes
// the original rich error chain.
type OriginalErrorer interface {
	OriginalError() error
}

// Original returns err's original error chain if err (or anything in its chain)
// implements OriginalErrorer; otherwise it returns err unchanged.
func Original(err error) error {
	var oe OriginalErrorer
	if errors.As(err, &oe) {
		return oe.OriginalError()
	}
	return err
}

// convertedError is returned by Convert. Its GRPCStatus() drives wire serialization
// (gRPC server framework prefers it over Error()), so the status message that
// reaches the client is the sanitized user message. Unwrap() returns the original
// rich error chain so errors.Is/As work, and OriginalError() exposes it explicitly
// for interceptors that want to log the technical chain instead of the user message.
type convertedError struct {
	grpcErr  error
	original error
}

func (e *convertedError) Error() string              { return e.grpcErr.Error() }
func (e *convertedError) Unwrap() error              { return e.original }
func (e *convertedError) GRPCStatus() *status.Status { s, _ := status.FromError(e.grpcErr); return s }

// OriginalError returns the original (rich) error passed to Convert, with its
// full chain preserved - for use in logging and observability.
func (e *convertedError) OriginalError() error { return e.original }

func getGRPCCode(err error) (codes.Code, bool) {
	if c, ok := codeCache.Get(err); ok {
		return *c, true
	}

	if err, ok := err.(Convertible); ok {
		return err.GRPCErrorCode(), true
	}

	if err, ok := err.(wrappedErr); ok {
		return getGRPCCode(err.Unwrap())
	}

	if err, ok := err.(wrappedErrs); ok {
		errs := err.Unwrap()
		codes := make(map[codes.Code]struct{}, len(errs))
		for _, err := range errs {
			if c, ok := getGRPCCode(err); ok {
				codes[c] = struct{}{}
			}
		}
		if len(codes) > 1 {
			panic("more wrapped errors provide a gRPC code") // what should we do here?
		}
		if len(codes) == 0 {
			return 0, false
		}
		for c := range codes {
			return codeCache.PutValue(err, c), true
		}
	}

	return 0, false
}

type wrappedErr interface {
	error
	Unwrap() error
}

type wrappedErrs interface {
	error
	Unwrap() []error
}

var _ OriginalErrorer = new(convertedError)
