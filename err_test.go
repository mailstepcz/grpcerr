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

		domainErr := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Do not press that button!",
		)
		convertedErr := Convert(domainErr)

		s, ok := status.FromError(convertedErr)
		req.True(ok)
		req.Equal(codes.NotFound, s.Code())
		req.Equal("Do not press that button!", s.Message())
	})

	t.Run("backward compat — no user message uses err.Error()", func(t *testing.T) {
		req := require.New(t)

		domainErr := Wrap("", errors.New("getting entity"), codes.NotFound)
		convertedErr := Convert(domainErr)

		s, ok := status.FromError(convertedErr)
		req.True(ok)
		req.Equal(codes.NotFound, s.Code())
		req.Equal("getting entity", s.Message())
	})

	t.Run("errors.Is traverses convertedError into rich chain", func(t *testing.T) {
		req := require.New(t)

		sentinelErr := errors.New("getting entity")
		domainErr := serr.WithUserMessage(
			Wrap("", sentinelErr, codes.NotFound),
			"Do not press that button!",
		)
		convertedErr := Convert(domainErr)

		req.True(errors.Is(convertedErr, sentinelErr))
	})

	t.Run("OriginalError exposes the rich chain", func(t *testing.T) {
		req := require.New(t)

		domainErr := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Do not press that button!",
		)
		convertedErr := Convert(domainErr)

		oe, ok := convertedErr.(OriginalErrorer)
		req.True(ok)
		req.Equal(domainErr, oe.OriginalError())
		req.Contains(oe.OriginalError().Error(), "getting entity")
	})

	t.Run("ExtractUserMessage on converted error works through Unwrap chain", func(t *testing.T) {
		req := require.New(t)

		domainErr := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Do not press that button!",
		)
		convertedErr := Convert(domainErr)
		req.Equal("Do not press that button!", serr.ExtractUserMessage(convertedErr))
	})

	t.Run("user message survives through serr.Wrap layer", func(t *testing.T) {
		req := require.New(t)

		domainErr := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Getting order.",
		)
		serviceErr := serr.Wrap("tx failed", domainErr, serr.String("id", uuid.New().String()))
		convertedErr := Convert(serviceErr)

		s, ok := status.FromError(convertedErr)
		req.True(ok)
		req.Equal(codes.NotFound, s.Code())
		req.Equal("Getting order.", s.Message())
	})
}

func TestOriginal(t *testing.T) {
	t.Run("converted error returns its rich chain", func(t *testing.T) {
		req := require.New(t)

		domainErr := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Entity was not found, please check ID.",
		)
		convertedErr := Convert(domainErr)

		req.Equal("rpc error: code = NotFound desc = Entity was not found, please check ID.", convertedErr.Error())
		req.Equal(domainErr, Original(convertedErr)) // should be 'getting entity'
	})

	t.Run("converted error returns its rich chain w/o user message", func(t *testing.T) {
		req := require.New(t)

		domainErr := Wrap("", errors.New("getting entity"), codes.NotFound)
		convertedErr := Convert(domainErr)

		req.Equal("rpc error: code = NotFound desc = getting entity", convertedErr.Error())
		req.Equal(domainErr, Original(convertedErr))
	})

	t.Run("plain error is returned unchanged", func(t *testing.T) {
		req := require.New(t)

		rawErr := errors.New("plain")
		req.Same(rawErr, Original(rawErr))
	})

	t.Run("nil returns nil", func(t *testing.T) {
		require.NoError(t, Original(nil))
	})

	t.Run("wrapped converted error is unwrapped via errors.As", func(t *testing.T) {
		req := require.New(t)

		domainErr := serr.WithUserMessage(
			Wrap("", errors.New("getting entity"), codes.NotFound),
			"Entity was not found, please check ID.",
		)
		convertedErr := Convert(domainErr)
		// outerErr layers a serr.Wrap on top of convertedErr; Original must
		// still reach OriginalErrorer via errors.As.
		outerErr := serr.Wrap("outer", convertedErr)

		req.Equal(domainErr, Original(outerErr)) // should be 'getting entity'
	})
}
