package patcher

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAveragerEmpty(t *testing.T) {
	a := NewAverager(4)
	require.Equal(t, 0.0, a.Average())
}

func TestAveragerWithinWindow(t *testing.T) {
	a := NewAverager(4)
	a.Add(1)
	a.Add(3)
	expected := 2.0
	actual := a.Average()
	require.InEpsilon(t, expected, actual, 0.01, "expected %f got %f", expected, actual)
}

func TestAveragerOutsideWindow(t *testing.T) {
	a := NewAverager(4)
	a.Add(1)
	a.Add(3)
	a.Add(5)
	a.Add(7)
	a.Add(9)
	a.Add(11)
	expected := 8.0
	actual := a.Average()
	require.InEpsilon(t, expected, actual, 0.01, "expected %f got %f", expected, actual)
}
