import sys
import asyncio
import json
import re
import socket
import argparse
from pathlib import Path
from urllib.parse import urljoin, urlparse
from urllib.request import urlopen, Request
from playwright.async_api import async_playwright, TimeoutError as PlaywrightTimeoutError  # type: ignore
import aiohttp  # type: ignore
import contextlib

async def run(cdp_url: str | None = None) -> None:
    """Run browser automation either by connecting to existing instance or launching new one."""
    async with async_playwright() as p:
        if cdp_url:
            # Connect to the running browser exposed via the CDP websocket URL.
            browser = await p.chromium.connect_over_cdp(cdp_url)
        else:
            # Launch a new browser instance locally with GUI
            browser = await p.chromium.launch(headless=False)

        # Re-use the first context if present, otherwise create a fresh one.
        if browser.contexts:
            context = browser.contexts[0]
        else:
            context = await browser.new_context()

        # Re-use the first page if present, otherwise create a fresh one.
        page = context.pages[0] if context.pages else await context.new_page()

        # Snapshot the page as-is for debugging purposes.
        print(f"Page URL: {page.url}")
        print(f"Taking screenshot before navigation")
        await page.screenshot(path="screenshot-before.png", full_page=True)

        # Decide destination URL.
        target_url = (
            "https://www.apple.com"
            if "apple.com" not in page.url
            else "https://www.microsoft.com"
        )

        print(f"Navigating to {target_url} …", file=sys.stderr)

        try:
            # First wait only for DOMContentLoaded – many modern sites keep long-polling
            # connections alive which makes the stricter "networkidle" heuristic unreliable.
            await page.goto(target_url, wait_until="domcontentloaded", timeout=60_000)

            # Optionally wait for a quieter network but don't fail if it never settles.
            try:
                await page.wait_for_load_state("networkidle", timeout=10_000)
            except PlaywrightTimeoutError:
                print("networkidle state not reached within 10 s – proceeding", file=sys.stderr)

        except PlaywrightTimeoutError:
            print(f"Navigation to {target_url} timed out after 60 s", file=sys.stderr)
            # Capture the state for post-mortem analysis.
            await page.screenshot(path="screenshot-error.png", full_page=True)
            raise

        # Ensure output directory and save screenshot.
        out_path = Path("screenshot.png")
        await page.screenshot(path=str(out_path), full_page=True)
        print(f"Screenshot saved to {out_path.resolve()}")

        # Navigate to IRS forms page and download Form 1040
        print(f"Navigating to IRS forms page...", file=sys.stderr)
        try:
            await page.goto("https://www.irs.gov/forms-instructions", wait_until="domcontentloaded", timeout=10_000)
            
            # Wait for network to settle
            try:
                await page.wait_for_load_state("networkidle", timeout=10_000)
            except PlaywrightTimeoutError:
                print("networkidle state not reached within 10 s – proceeding", file=sys.stderr)
            
            print("Looking for Form 1040 download link...", file=sys.stderr)
            
            # Start waiting for the download
            async with page.expect_download(timeout=10_000) as download_info:
                # Click on <a> tag with title containing "Form 1040" to initiate download
                form_link = page.locator('a[title*="Form 1040"]').first
                print(f"Found <a> tag with title containing 'Form 1040'", file=sys.stderr)
                await form_link.click(timeout=5_000)
                print(f"Clicked on Form 1040 link", file=sys.stderr)
            download = await download_info.value
            print(f"Download received", file=sys.stderr)
            
            # Save the downloaded file
            download_path = Path("downloads")
            download_path.mkdir(exist_ok=True)
            save_path = download_path / download.suggested_filename
            await download.save_as(str(save_path))
            print(f"Form 1040 downloaded and saved to {save_path.resolve()}")
            
        except PlaywrightTimeoutError:
            print(f"Operations on IRS forms page timed out", file=sys.stderr)
            await page.screenshot(path="screenshot-irs-error.png", full_page=True)
        except Exception as e:
            print(f"Error downloading Form 1040: {e}", file=sys.stderr)
            await page.screenshot(path="screenshot-download-error.png", full_page=True)

        await browser.close()


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

        # Chromium devtools HTTP server only accepts Host headers that are an
        # IP literal or "localhost".  If the caller passed a hostname, resolve
        # it to an IP so that the request is not rejected.
        parsed = urlparse(version_url)
        raw_host = parsed.hostname or "localhost"
        # Quick-and-dirty IP-literal check (IPv4 or bracket-less IPv6).
        _IP_RE = re.compile(r"^(?:\d+\.\d+\.\d+\.\d+|[0-9a-fA-F:]+)$")
        if raw_host != "localhost" and not _IP_RE.match(raw_host):
            try:
                raw_host = socket.gethostbyname(raw_host)
            except Exception:  # noqa: BLE001
                # Fall back to localhost if resolution fails; devtools handler
                # will at least accept it rather than closing the connection.
                raw_host = "localhost"
        host_header = raw_host
        if parsed.port:
            host_header = f"{host_header}:{parsed.port}"
        print(f"Host header: {host_header}")
        req = Request(version_url, headers={"Host": host_header})
        with urlopen(req) as resp:
            data = json.load(resp)
        print(f"Data: {data}")
        # change ws:// to ws:// if parsed was https. Also change IP back to the hostname
        if parsed.scheme == "https":
            data["webSocketDebuggerUrl"] = data["webSocketDebuggerUrl"].replace("ws://", "wss://")
            data["webSocketDebuggerUrl"] = data["webSocketDebuggerUrl"].replace(raw_host, parsed.hostname)
        print(f"debugger url: {data['webSocketDebuggerUrl']}")
        return data["webSocketDebuggerUrl"]
    except Exception as exc:  # noqa: BLE001
        print(
            f"Failed to retrieve webSocketDebuggerUrl from {version_url}: {exc}",
            file=sys.stderr,
        )
        sys.exit(1)

# ---------------- keep-alive task ---------------- #


async def _keep_alive(endpoint: str) -> None:
    """Periodically send a GET request to *endpoint* to keep the instance alive."""
    # Ensure scheme; default to http:// if missing.
    if not endpoint.startswith(("http://", "https://")):
        endpoint = f"http://{endpoint}"

    async with aiohttp.ClientSession() as session:
        while True:
            try:
                async with session.get(endpoint) as resp:
                    # Consume the response body to finish the request.
                    await resp.read()
            except Exception as exc:  # noqa: BLE001
                print(f"Keep-alive request to {endpoint} failed: {exc}", file=sys.stderr)

            await asyncio.sleep(1)


async def _async_main(endpoint_arg: str | None, local: bool) -> None:
    """Run browser automation either locally or via CDP connection."""
    
    if local:
        # Run locally without CDP connection or keep-alive
        await run()
    else:
        # Resolve CDP URL and use keep-alive for remote connection
        cdp_url = _resolve_cdp_url(endpoint_arg)
        
        # Start the keep-alive loop.
        keep_alive_task = asyncio.create_task(_keep_alive(endpoint_arg))
        
        try:
            await run(cdp_url)
        finally:
            # Ensure the keep-alive task is cancelled cleanly when run() completes.
            keep_alive_task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await keep_alive_task

def main() -> None:
    parser = argparse.ArgumentParser(
        description="Browser automation script with CDP or local Chromium",
        epilog="Examples:\n"
               "  CDP mode:   python main.py localhost:9222\n"
               "  Local mode: python main.py --local",
        formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("endpoint", nargs="?", help="DevTools HTTP endpoint (e.g., localhost:9222)")
    parser.add_argument("--local", action="store_true", help="Launch Chromium locally with GUI instead of connecting via CDP")
    args = parser.parse_args()
    
    if not args.local and not args.endpoint:
        parser.error("Either provide an endpoint or use --local flag")
    
    asyncio.run(_async_main(args.endpoint, args.local))

if __name__ == "__main__":
    main()
