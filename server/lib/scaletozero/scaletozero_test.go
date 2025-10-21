package scaletozero

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebouncedControllerSingleDisableEnable(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Enable(context.Background()))

	assert.Equal(t, 1, mock.disableCalls)
	assert.Equal(t, 1, mock.enableCalls)
}

func TestDebouncedControllerMultipleDisablesDebounced(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Disable(context.Background()))

	assert.Equal(t, 1, mock.disableCalls)
}

func TestDebouncedControllerEnableOnlyOnLastHolder(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Enable(context.Background()))
	assert.Equal(t, 0, mock.enableCalls)

	require.NoError(t, c.Enable(context.Background()))
	assert.Equal(t, 1, mock.enableCalls)
}

func TestDebouncedControllerDisableFailureRollsBack(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{disableErr: assert.AnError}
	c := NewDebouncedController(mock)

	err := c.Disable(context.Background())
	require.Error(t, err)
	assert.Equal(t, 1, mock.disableCalls)

	// Clear error; next Disable should write again
	mock.disableErr = nil
	require.NoError(t, c.Disable(context.Background()))
	assert.Equal(t, 2, mock.disableCalls)

	// Enable should write once
	require.NoError(t, c.Enable(context.Background()))
	assert.Equal(t, 1, mock.enableCalls)
}

func TestDebouncedControllerEnableFailureRetry(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(context.Background()))
	mock.enableErr = assert.AnError

	err := c.Enable(context.Background())
	require.Error(t, err)
	assert.Equal(t, 1, mock.enableCalls)

	// Clear error; retry should succeed
	mock.enableErr = nil
	require.NoError(t, c.Enable(context.Background()))
	assert.Equal(t, 2, mock.enableCalls)
}

func TestDebouncedControllerEnableWithoutDisableNoWrite(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)
	require.NoError(t, c.Enable(context.Background()))
	assert.Equal(t, 0, mock.enableCalls)
}

func TestDebouncedControllerInterleavedSequence(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)
	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Enable(context.Background()))
	require.NoError(t, c.Disable(context.Background()))
	require.NoError(t, c.Enable(context.Background()))
	assert.Equal(t, 2, mock.disableCalls)
	assert.Equal(t, 2, mock.enableCalls)
}

type mockScaleToZeroer struct {
	mu           sync.Mutex
	disableCalls int
	enableCalls  int
	disableErr   error
	enableErr    error
}

func (m *mockScaleToZeroer) Disable(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disableCalls++
	return m.disableErr
}

func (m *mockScaleToZeroer) Enable(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enableCalls++
	return m.enableErr
}
