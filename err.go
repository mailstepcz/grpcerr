package grpcerr

import (
	"database/sql"
	"errors"

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
func New(err string, code codes.Code) ConvertibleError {
	return Wrap(errors.New(err), code)
}

// Wrap wraps an error into one convertible into a gRPC error.
func Wrap(err error, code codes.Code) ConvertibleError {
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

// Convert converts the error into a gRPC error.
func Convert(err error) error {
	if c, ok := getGRPCCode(err); ok {
		return status.Error(c, err.Error())
	}

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return status.Error(codes.NotFound, err.Error())
	}

	return status.Error(codes.Internal, err.Error())
}

func getGRPCCode(err error) (codes.Code, bool) {
	if err, ok := err.(Convertible); ok {
		return err.GRPCErrorCode(), true
	}

	if err, ok := err.(wrappedErr); ok {
		return getGRPCCode(err.Unwrap())
	}

	if err, ok := err.(wrappedErrs); ok {
		errs := err.Unwrap()
		codes := make([]codes.Code, 0, len(errs))
		for _, err := range errs {
			if c, ok := getGRPCCode(err); ok {
				codes = append(codes, c)
			}
		}
		if len(codes) > 1 {
			panic("more wrapped errors provide a gRPC code") // what should we do here?
		}
		if len(codes) == 0 {
			return 0, false
		}
		return codes[0], true
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
