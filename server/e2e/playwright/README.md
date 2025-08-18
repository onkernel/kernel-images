# Playwright CDP Integration

This directory contains a Playwright-based script that replaces the chromedp functionality in the e2e tests.

## Installation

```bash
pnpm install
```

## Usage

The script connects to an existing Chrome browser instance via CDP (Chrome DevTools Protocol) and performs various browser automation tasks.

### Commands

#### navigate-and-ensure-cookie

Navigates to a URL and ensures a specific cookie exists with the expected value:

```bash
pnpm exec tsx index.ts navigate-and-ensure-cookie \
  --url "http://localhost:8080/set-cookie" \
  --cookie-name "session_id" \
  --cookie-value "abc123" \
  --label "test-label" \
  --ws-url "ws://127.0.0.1:9222/" \
  --timeout 45000
```

Options:
- `--url` (required): The URL to navigate to
- `--cookie-name` (required): The name of the cookie to check
- `--cookie-value` (required): The expected value of the cookie
- `--label` (optional): Label for screenshot filenames on failure
- `--ws-url` (optional): WebSocket URL for CDP connection (default: ws://127.0.0.1:9222/)
- `--timeout` (optional): Timeout in milliseconds (default: 45000)

#### capture-screenshot

Takes a full-page screenshot:

```bash
pnpm exec tsx index.ts capture-screenshot \
  --filename "screenshot.png" \
  --ws-url "ws://127.0.0.1:9222/"
```

Options:
- `--filename` (required): Output filename for the screenshot
- `--ws-url` (optional): WebSocket URL for CDP connection (default: ws://127.0.0.1:9222/)

## Integration with Go Tests

The Go e2e tests execute this script via `exec.Command` to perform browser automation tasks. The script:

1. Connects to an existing Chrome instance (not launching a new one)
2. Reuses existing browser contexts and pages when possible
3. Returns appropriate exit codes (0 for success, 1 for failure)
4. Outputs logs to stdout/stderr for debugging

## Development

To test the script locally:

1. Start a Chrome instance with remote debugging enabled:
   ```bash
   chromium --remote-debugging-port=9222
   ```

2. Run the script with desired arguments:
   ```bash
   pnpm exec tsx index.ts <command> [options]
   ```
