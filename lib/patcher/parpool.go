package patcher

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// DoInParallelWithResult runs the execute function for each element in the input slice,
// processing at most numWorkers at a time. If one of the workers errors the context
// passed to the others is cancelled.
//
// The execute func should return context.Canceled if it stops due to context being
// cancelled.
//
// The error returned is an error of a failing worker. If the whole operation
// is cancelled through the ctx context the error is context.Canceled.
func DoInParallelWithResult[TIn any, TOut any](
	ctx context.Context,
	execute func(context.Context, TIn) (TOut, error),
	input []TIn,
	numWorkers int,
) ([]TOut, error) {
	// See https://pkg.go.dev/golang.org/x/sync/errgroup#example-Group-Parallel
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(numWorkers)
	output := make([]TOut, len(input))
	for i, val := range input {
		i, val := i, val // Prevent loop variable closure problem
		g.Go(func() error {
			result, err := execute(ctx, val)
			if err == nil {
				output[i] = result
			}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return output, nil
}

// DoInParallel is like DoInParallelWithResult but without collecting results.
func DoInParallel[TIn any](
	ctx context.Context,
	execute func(context.Context, TIn) error,
	input []TIn,
	numWorkers int,
) error {
	_, err := DoInParallelWithResult[TIn, struct{}](ctx, func(ctx context.Context, v TIn) (struct{}, error) {
		return struct{}{}, execute(ctx, v)
	}, input, numWorkers)
	return err
}
