package grpcerr

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestIsCanceled(t *testing.T) {
	tcs := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "plain context.Canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "serr-wrapped context.Canceled",
			err:  serr.Wrap("loading entity", context.Canceled),
			want: true,
		},
		{
			name: "gRPC status carrying codes.Canceled",
			err:  status.Error(codes.Canceled, "context canceled"),
			want: true,
		},
		{
			name: "serr-wrapped gRPC status carrying codes.Canceled",
			err:  serr.Wrap("getting users by ids", status.Error(codes.Canceled, "context canceled")),
			want: true,
		},
		{
			name: "converted error is inspected through its original chain",
			err:  Convert(serr.Wrap("getting users by ids", status.Error(codes.Canceled, "context canceled"))),
			want: true,
		},
		{
			name: "postgres query_canceled SQLSTATE",
			err:  serr.Wrap("listing timelogs", &pgconn.PgError{Code: pgerrcode.QueryCanceled}),
			want: true,
		},
		{
			name: "postgres error with another SQLSTATE",
			err:  serr.Wrap("listing timelogs", &pgconn.PgError{Code: pgerrcode.DeadlockDetected}),
			want: false,
		},
		{
			name: "explicitly tagged codes.Canceled",
			err:  Wrap("", errors.New("caller went away"), codes.Canceled),
			want: true,
		},
		{
			name: "explicit code wins over a cancelled cause",
			err:  Wrap("", status.Error(codes.Canceled, "context canceled"), codes.Internal),
			want: false,
		},
		{
			name: "context.DeadlineExceeded is not a cancellation",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "gRPC status carrying codes.DeadlineExceeded is not a cancellation",
			err:  status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			want: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsCanceled(tc.err))
		})
	}
}

// TestIsCanceledCoversWhatErrorsIsMisses documents the cancellation forms that a plain
// errors.Is(err, context.Canceled) check does not catch.
func TestIsCanceledCoversWhatErrorsIsMisses(t *testing.T) {
	tcs := []struct {
		name string
		err  error
	}{
		{
			name: "gRPC status carrying codes.Canceled",
			err:  status.Error(codes.Canceled, "context canceled"),
		},
		{
			name: "postgres query_canceled SQLSTATE",
			err:  &pgconn.PgError{Code: pgerrcode.QueryCanceled},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)

			req.False(errors.Is(tc.err, context.Canceled))
			req.True(IsCanceled(tc.err))
		})
	}
}

func TestConvertCanceled(t *testing.T) {
	t.Run("downstream gRPC Canceled no longer becomes Internal", func(t *testing.T) {
		req := require.New(t)

		// the production shape: a cancelled call to another service, wrapped by the
		// service layer and given a user message before reaching the handler
		serviceErr := serr.WithUserMessage(
			serr.Wrap("enriching ranked users with details",
				serr.Wrap("getting users details from user service",
					status.Error(codes.Canceled, "context canceled"))),
			"Unable to load productivity ranking.",
		)

		s, ok := status.FromError(Convert(serviceErr))
		req.True(ok)
		req.Equal(codes.Canceled, s.Code())
		req.Equal("Unable to load productivity ranking.", s.Message())
	})

	t.Run("postgres query_canceled maps to codes.Canceled", func(t *testing.T) {
		req := require.New(t)

		serviceErr := serr.Wrap("listing timelogs", &pgconn.PgError{Code: pgerrcode.QueryCanceled})

		req.Equal(codes.Canceled, status.Code(Convert(serviceErr)))
	})

	t.Run("an unrelated failure still maps to codes.Internal", func(t *testing.T) {
		req := require.New(t)

		req.Equal(codes.Internal, status.Code(Convert(serr.Wrap("listing timelogs", errors.New("boom")))))
	})
}
