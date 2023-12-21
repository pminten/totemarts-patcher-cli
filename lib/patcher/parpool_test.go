package patcher

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParpoolOk(t *testing.T) {
	execute := func(ctx context.Context, a int64) (string, error) { return strconv.FormatInt(a, 10), nil }
	actual, err := DoInParallelWithResult[int64, string](context.Background(), execute, []int64{1, 2, 3, 4, 5}, 2)
	expected := []string{"1", "2", "3", "4", "5"}
	require.NoError(t, err)
	require.EqualValues(t, expected, actual)
}

func TestParpoolSingleError(t *testing.T) {
	execute := func(ctx context.Context, a int64) (string, error) {
		if a == 3 {
			return "", errors.New("no three")
		}
		return strconv.FormatInt(a, 10), nil
	}
	_, err := DoInParallelWithResult[int64, string](context.Background(), execute, []int64{1, 2, 3, 4, 5}, 2)
	require.ErrorContains(t, err, "no three")
}

func TestParpoolCancelled(t *testing.T) {
	execute := func(ctx context.Context, a int64) (string, error) {
		select {
		case <-ctx.Done():
			return "", context.Canceled
		case <-time.After(10 * time.Second):
			// Merely to ensure that eventually the test fails.
			return "", errors.New("timeout reached")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Bit ugly, sleep in tests.
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	_, err := DoInParallelWithResult[int64, string](ctx, execute, []int64{1, 2, 3, 4, 5}, 2)
	require.ErrorIs(t, err, context.Canceled)
}

func TestParpoolNoResult(t *testing.T) {
	m := sync.Mutex{}
	res := 0
	execute := func(ctx context.Context, a int) error {
		m.Lock()
		defer m.Unlock()
		// Because + is commutative and associative it's safe to use with the indeterminate order here.
		res += a
		return nil
	}
	err := DoInParallel[int](context.Background(), execute, []int{1, 2, 3, 4, 5}, 2)
	require.NoError(t, err)
	require.Equal(t, 15, res)
}
