package grpcerr

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/mailstepcz/serr"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestConvert(t *testing.T) {
	req := require.New(t)

	dummyErr := Wrap("", errors.ErrUnsupported, codes.Unimplemented)

	req.Equal(codes.Unimplemented, status.Code(Convert(dummyErr)))

	req.Equal("unsupported operation", dummyErr.Error())

	req.True(errors.Is(dummyErr, errors.ErrUnsupported))

	req.Equal(codes.NotFound, status.Code(Convert(sql.ErrNoRows)))
}

func TestConvertWrappedError(t *testing.T) {
	req := require.New(t)

	dummyErr := serr.Wrap("wrapped", Wrap("", errors.ErrUnsupported, codes.Unimplemented))

	req.Equal(codes.Unimplemented, status.Code(Convert(dummyErr)))

	req.Equal("wrapped: unsupported operation", dummyErr.Error())

	req.True(errors.Is(dummyErr, errors.ErrUnsupported))

	req.Equal(codes.NotFound, status.Code(Convert(serr.Wrap("wrapped", sql.ErrNoRows))))
}

func TestConvertWrappedErrors(t *testing.T) {
	req := require.New(t)

	dummyErr := errors.Join(errors.New("some error"), Wrap("", errors.ErrUnsupported, codes.Unimplemented))

	req.Equal(codes.Unimplemented, status.Code(Convert(dummyErr)))

	req.Equal("some error\nunsupported operation", dummyErr.Error())

	req.True(errors.Is(dummyErr, errors.ErrUnsupported))

	req.Equal(codes.NotFound, status.Code(Convert(errors.Join(errors.New("some error"), sql.ErrNoRows))))
}
