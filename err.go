package grpcerr

import (
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
	errorCache cache.Cache[error, error]
	codeCache  cache.Cache[error, codes.Code]
)

// Convert converts the error into a gRPC error.
func Convert(err error) error {
	// if err, ok := errorCache.Get(err); ok {
	// 	return *err
	// }

	if c, ok := getGRPCCode(err); ok {
		return errorCache.PutValue(err, status.Error(c, err.Error()))
	}

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return errorCache.PutValue(err, status.Error(codes.NotFound, err.Error()))
	}

	return errorCache.PutValue(err, status.Error(codes.Internal, err.Error()))
}

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
