package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func sumSteps(steps [][2]int) (int, int) {
	sx, sy := 0, 0
	for _, s := range steps {
		sx += s[0]
		sy += s[1]
	}
	return sx, sy
}

func countSteps(steps [][2]int) int { return len(steps) }

func TestGenerateRelativeSteps_Zero(t *testing.T) {
	steps := generateRelativeSteps(0, 0, 5)
	require.Len(t, steps, 0, "expected 0 steps")
}

func TestGenerateRelativeSteps_AxisAligned(t *testing.T) {
	cases := []struct {
		dx, dy int
	}{
		{5, 0}, {-7, 0}, {0, 9}, {0, -3},
	}
	for _, c := range cases {
		steps := generateRelativeSteps(c.dx, c.dy, 5)
		sx, sy := sumSteps(steps)
		require.Equal(t, c.dx, sx, "sum mismatch dx")
		require.Equal(t, c.dy, sy, "sum mismatch dy")
		require.Equal(t, 5, countSteps(steps), "count mismatch")
	}
}

func TestGenerateRelativeSteps_DiagonalsAndSlopes(t *testing.T) {
	cases := []struct{ dx, dy int }{
		{5, 5}, {-4, -4}, {8, 3}, {3, 8}, {-9, 2}, {2, -9},
	}
	for _, c := range cases {
		steps := generateRelativeSteps(c.dx, c.dy, 5)
		sx, sy := sumSteps(steps)
		require.Equal(t, c.dx, sx, "sum mismatch dx")
		require.Equal(t, c.dy, sy, "sum mismatch dy")
		require.Equal(t, 5, countSteps(steps), "count mismatch")
	}
}
