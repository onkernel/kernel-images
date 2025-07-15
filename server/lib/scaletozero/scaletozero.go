package scaletozero

import (
	"context"
	"os"
	"sync"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

// Unikraft scale-to-zero control file
// https://unikraft.cloud/docs/api/v1/instances/#scaletozero_app
const unikraftScaleToZeroFile = "/uk/libukp/scale_to_zero_disable"

type Controller interface {
	// Disable turns scale-to-zero off.
	Disable(ctx context.Context) error
	// Enable re-enables scale-to-zero after it has previously been disabled.
	Enable(ctx context.Context) error
}

type unikraftCloudController struct {
	path string
}

func NewUnikraftCloudController() Controller {
	return &unikraftCloudController{path: unikraftScaleToZeroFile}
}

func (c *unikraftCloudController) Disable(ctx context.Context) error {
	return c.write(ctx, "+")
}

func (c *unikraftCloudController) Enable(ctx context.Context) error {
	return c.write(ctx, "-")
}

func (c *unikraftCloudController) write(ctx context.Context, char string) error {
	if _, err := os.Stat(c.path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logger.FromContext(ctx).Error("failed to stat scale-to-zero control file", "path", c.path, "err", err)
		return err
	}

	f, err := os.OpenFile(c.path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		logger.FromContext(ctx).Error("failed to open scale-to-zero control file", "path", c.path, "err", err)
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte(char)); err != nil {
		logger.FromContext(ctx).Error("failed to write scale-to-zero control file", "path", c.path, "err", err)
		return err
	}
	return nil
}

type NoopController struct{}

func NewNoopController() *NoopController { return &NoopController{} }

func (NoopController) Disable(context.Context) error { return nil }
func (NoopController) Enable(context.Context) error  { return nil }

// Oncer wraps a Controller and ensures that Disable and Enable are called at most once.
type Oncer struct {
	ctrl        Controller
	disableOnce sync.Once
	enableOnce  sync.Once
	disableErr  error
	enableErr   error
}

func NewOncer(c Controller) *Oncer { return &Oncer{ctrl: c} }

func (o *Oncer) Disable(ctx context.Context) error {
	o.disableOnce.Do(func() { o.disableErr = o.ctrl.Disable(ctx) })
	return o.disableErr
}

func (o *Oncer) Enable(ctx context.Context) error {
	o.enableOnce.Do(func() { o.enableErr = o.ctrl.Enable(ctx) })
	return o.enableErr
}
