import sys
import asyncio
import json
from pathlib import Path
from urllib.parse import urljoin
from urllib.request import urlopen
from playwright.async_api import async_playwright

async def run(cdp_url: str) -> None:
    """Connect to an existing Chromium instance via CDP, navigate, and screenshot."""
    async with async_playwright() as p:
        # Connect to the running browser exposed via the CDP websocket URL.
        browser = await p.chromium.connect_over_cdp(cdp_url)

        # Re-use the first context if present, otherwise create a fresh one.
        if browser.contexts:
            context = browser.contexts[0]
        else:
            context = await browser.new_context()

        # Re-use the first page if present, otherwise create a fresh one.
        page = context.pages[0] if context.pages else await context.new_page()

        # Navigate to Hacker News.
        await page.goto("https://news.ycombinator.com", wait_until="networkidle")

        # Ensure output directory and save screenshot.
        out_path = Path("screenshot.png")
        await page.screenshot(path=str(out_path), full_page=True)
        print(f"Screenshot saved to {out_path.resolve()}")

        await context.close()


# ---------------- CLI entrypoint ---------------- #

def _resolve_cdp_url(arg: str) -> str:
    """Resolve the provided argument to a CDP websocket URL.

    If *arg* already looks like a ws:// or wss:// URL, return it unchanged.
    Otherwise, treat it as a DevTools HTTP endpoint (e.g. http://localhost:9222
    or just localhost:9222), fetch /json/version, and extract the
    'webSocketDebuggerUrl'.
    """

    # Ensure scheme. Default to http:// if none supplied.
    if not arg.startswith(("http://", "https://")):
        arg = f"http://{arg}"

    version_url = urljoin(arg.rstrip("/") + "/", "json/version")
    try:
        with urlopen(version_url) as resp:
            data = json.load(resp)
        return data["webSocketDebuggerUrl"]
    except Exception as exc:  # noqa: BLE001
        print(
            f"Failed to retrieve webSocketDebuggerUrl from {version_url}: {exc}",
            file=sys.stderr,
        )
        sys.exit(1)


def main() -> None:
    if len(sys.argv) < 2:
        print("Usage: python main.py <DevTools HTTP endpoint>", file=sys.stderr)
        sys.exit(1)
    cdp_url = _resolve_cdp_url(sys.argv[1])
    asyncio.run(run(cdp_url))

if __name__ == "__main__":
    main()
