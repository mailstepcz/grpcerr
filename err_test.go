package grpcerr

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
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

	req.Equal(codes.Canceled.String(), status.Code(Convert(context.Canceled)).String())
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

func TestConvertWithUserMessage(t *testing.T) {
	t.Run("user message becomes gRPC status message", func(t *testing.T) {
		req := require.New(t)

		domain := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Do not press that button!",
		)
		converted := Convert(domain)

		s, ok := status.FromError(converted)
		req.True(ok)
		req.Equal(codes.NotFound, s.Code())
		req.Equal("Do not press that button!", s.Message())
	})

	t.Run("backward compat — no user message uses err.Error()", func(t *testing.T) {
		req := require.New(t)

		domain := Wrap("", errors.New("getting entity"), codes.NotFound)
		converted := Convert(domain)

		s, ok := status.FromError(converted)
		req.True(ok)
		req.Equal(codes.NotFound, s.Code())
		req.Equal("getting entity", s.Message())
	})

	t.Run("errors.Is traverses convertedError into rich chain", func(t *testing.T) {
		req := require.New(t)

		sentinel := errors.New("getting entity")
		domain := serr.WithUserMessage(
			Wrap("", sentinel, codes.NotFound),
			"Do not press that button!",
		)
		converted := Convert(domain)

		req.True(errors.Is(converted, sentinel))
	})

	t.Run("OriginalError exposes the rich chain", func(t *testing.T) {
		req := require.New(t)

		domain := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Do not press that button!",
		)
		converted := Convert(domain)

		oe, ok := converted.(interface{ OriginalError() error })
		req.True(ok)
		req.Equal(domain, oe.OriginalError())
		req.Contains(oe.OriginalError().Error(), "getting entity")
	})

	t.Run("ExtractUserMessage on converted error works through Unwrap chain", func(t *testing.T) {
		req := require.New(t)

		domain := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Do not press that button!",
		)
		converted := Convert(domain)
		req.Equal("Do not press that button!", serr.ExtractUserMessage(converted))
	})

	t.Run("user message survives through serr.Wrap layer", func(t *testing.T) {
		req := require.New(t)

		domain := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Getting order.",
		)
		serviceErr := serr.Wrap("tx failed", domain, serr.String("id", uuid.New().String()))
		converted := Convert(serviceErr)

		s, ok := status.FromError(converted)
		req.True(ok)
		req.Equal(codes.NotFound, s.Code())
		req.Equal("Getting order.", s.Message())
	})
}
