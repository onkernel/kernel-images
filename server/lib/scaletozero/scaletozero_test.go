package scaletozero

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebouncedControllerSingleDisableEnable(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Enable(t.Context()))

	assert.Equal(t, 1, mock.disableCalls)
	assert.Equal(t, 1, mock.enableCalls)
}

func TestDebouncedControllerMultipleDisablesDebounced(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Disable(t.Context()))

	assert.Equal(t, 1, mock.disableCalls)
}

func TestDebouncedControllerEnableOnlyOnLastHolder(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Enable(t.Context()))
	assert.Equal(t, 0, mock.enableCalls)

	require.NoError(t, c.Enable(t.Context()))
	assert.Equal(t, 1, mock.enableCalls)
}

func TestDebouncedControllerDisableFailureRollsBack(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{disableErr: assert.AnError}
	c := NewDebouncedController(mock)

	err := c.Disable(t.Context())
	require.Error(t, err)
	assert.Equal(t, 1, mock.disableCalls)

	// Clear error; next Disable should write again
	mock.disableErr = nil
	require.NoError(t, c.Disable(t.Context()))
	assert.Equal(t, 2, mock.disableCalls)

	// Enable should write once
	require.NoError(t, c.Enable(t.Context()))
	assert.Equal(t, 1, mock.enableCalls)
}

func TestDebouncedControllerEnableFailureRetry(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)

	require.NoError(t, c.Disable(t.Context()))
	mock.enableErr = assert.AnError

	err := c.Enable(t.Context())
	require.Error(t, err)
	assert.Equal(t, 1, mock.enableCalls)

	// Clear error; retry should succeed
	mock.enableErr = nil
	require.NoError(t, c.Enable(t.Context()))
	assert.Equal(t, 2, mock.enableCalls)
}

func TestDebouncedControllerEnableWithoutDisableNoWrite(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)
	require.NoError(t, c.Enable(t.Context()))
	assert.Equal(t, 0, mock.enableCalls)
}

func TestDebouncedControllerInterleavedSequence(t *testing.T) {
	t.Parallel()
	mock := &mockScaleToZeroer{}
	c := NewDebouncedController(mock)
	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Enable(t.Context()))
	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Enable(t.Context()))
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
func TestUnikraftCloudControllerNoFileNoError(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "scale_to_zero_disable")
	c := &unikraftCloudController{path: p}

	require.NoError(t, c.Disable(t.Context()))
	require.NoError(t, c.Enable(t.Context()))

	_, err := os.Stat(p)
	assert.True(t, os.IsNotExist(err), "should not create the file on no-op")
}

func TestUnikraftCloudControllerWritesPlusAndMinus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "scale_to_zero_disable")
	require.NoError(t, os.WriteFile(p, []byte{}, 0o600))
	c := &unikraftCloudController{path: p}

	require.NoError(t, c.Disable(t.Context()))
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, []byte("+"), b)

	require.NoError(t, c.Enable(t.Context()))
	b, err = os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, []byte("-"), b)
}

func TestUnikraftCloudControllerTruncatesExistingContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "scale_to_zero_disable")
	require.NoError(t, os.WriteFile(p, []byte("abc123"), 0o600))
	c := &unikraftCloudController{path: p}

	require.NoError(t, c.Disable(t.Context()))
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, []byte("+"), b)
}
