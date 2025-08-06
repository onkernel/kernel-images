import sys
import asyncio
import json
import re
import socket
import argparse
from pathlib import Path
from urllib.parse import urljoin, urlparse
from urllib.request import urlopen, Request
from playwright.async_api import Route, async_playwright, TimeoutError as PlaywrightTimeoutError  # type: ignore
import aiohttp  # type: ignore
import contextlib
from typing import Optional

# -------------- helper functions -------------- #

async def _click_and_await_download(page, client, click_func, timeout: int = 30):
    """Trigger *click_func* and wait until the browser download completes.

    Returns the suggested filename or None if unavailable/timed out.
    """
    download_completed = asyncio.Event()
    download_filename: str | None = None

    def _on_download_begin(event):
        nonlocal download_filename
        download_filename = event.get("suggestedFilename", "unknown")
        print(f"Download started: {download_filename}", file=sys.stderr)

    def _on_download_progress(event):
        if event.get("state") in ["completed", "canceled"]:
            download_completed.set()

    client.on("Browser.downloadWillBegin", _on_download_begin)
    client.on("Browser.downloadProgress", _on_download_progress)

    # Trigger the click that initiates the download.
    await click_func()

    try:
        await asyncio.wait_for(download_completed.wait(), timeout=timeout)
        print("Download completed", file=sys.stderr)
    except asyncio.TimeoutError:
        print(f"Download timed out after {timeout} seconds", file=sys.stderr)
    finally:
        # Best-effort removal of listeners (not critical).
        try:
            client.off("Browser.downloadWillBegin", _on_download_begin)  # type: ignore[attr-defined]
            client.off("Browser.downloadProgress", _on_download_progress)  # type: ignore[attr-defined]
        except Exception:
            pass

    return download_filename


async def _fetch_remote_downloads(cdp_url: str, remote_dir: str = "/tmp/downloads", local_dir: str = "downloads") -> None:
    """Fetch all files from *remote_dir* (over the filesystem API) and save them to *local_dir*."""
    parsed = urlparse(cdp_url)
    fs_base_url = f"https://{parsed.hostname}:444"

    async with aiohttp.ClientSession() as session:
        list_url = f"{fs_base_url}/fs/list_files?path={remote_dir}"
        print(f"Listing files in {remote_dir} from {list_url}", file=sys.stderr)

        async with session.get(list_url) as resp:
            if resp.status == 200:
                files = await resp.json()
                print(f"Found {len(files)} items in {remote_dir}", file=sys.stderr)
            elif resp.status == 404:
                print(f"{remote_dir} directory not found or empty", file=sys.stderr)
                files = []
            else:
                error_text = await resp.text()
                print(f"Failed to list files: {resp.status} - {error_text}", file=sys.stderr)
                files = []

            local_download_path = Path(local_dir)
            local_download_path.mkdir(exist_ok=True)

            for file_info in files:
                if not file_info.get("is_dir", False):
                    file_path = file_info.get("path")
                    file_name = file_info.get("name")

                    if file_path and file_name:
                        file_url = f"{fs_base_url}/fs/read_file?path={file_path}"
                        print(f"Fetching {file_name} from {file_url}", file=sys.stderr)

                        async with session.get(file_url) as file_resp:
                            if file_resp.status == 200:
                                content = await file_resp.read()
                                local_file_path = local_download_path / file_name
                                with open(local_file_path, "wb") as f:
                                    f.write(content)
                                print(f"Downloaded file saved to {local_file_path.resolve()}")
                            else:
                                error_text = await file_resp.text()
                                print(f"Failed to fetch {file_name}: {file_resp.status} - {error_text}", file=sys.stderr)

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

        # Get CDP client from page
        client = await page.context.new_cdp_session(page)
        
        # Configure download behavior
        await client.send(
            "Browser.setDownloadBehavior",
            {
                "behavior": "allow",
                "downloadPath": "/tmp/downloads",
                "eventsEnabled": True,
            },
        )
        print("Set download behavior via CDP", file=sys.stderr)

        # Intercept PDF requests to ensure they're downloaded as attachments
        async def handle_pdf_route(route: Route):
            response = await route.fetch()
            headers = response.headers
            headers['content-disposition'] = 'attachment'
            await route.fulfill(
                response=response,
                headers=headers
            )
        
        # Set up route interception for PDF files
        await page.route('**/*.pdf', handle_pdf_route)
        print("Set up PDF download interception", file=sys.stderr)

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

        # Navigate to the test page
        print(f"Navigating to download test page...", file=sys.stderr)
        try:
            await page.goto("https://browser-tests-alpha.vercel.app/api/download-test", wait_until="domcontentloaded", timeout=30_000)
            
            # Wait for network to settle
            try:
                await page.wait_for_load_state("networkidle", timeout=10_000)
            except PlaywrightTimeoutError:
                print("networkidle state not reached within 10 s – proceeding", file=sys.stderr)
            
            # Trigger the first test download via helper
            await _click_and_await_download(
                page,
                client,
                lambda: page.click("#download"),
            )

            # Fetch the downloaded files from the filesystem API (remote mode only)
            if cdp_url:
                await _fetch_remote_downloads(cdp_url)

            # --------------- Second download: IRS Form 1040 --------------- #
            print("Navigating to IRS Forms & Instructions page...", file=sys.stderr)
            try:
                await page.goto("https://www.irs.gov/forms-instructions", wait_until="domcontentloaded", timeout=30_000)
                try:
                    await page.wait_for_load_state("networkidle", timeout=10_000)
                except PlaywrightTimeoutError:
                    print("networkidle state not reached within 10 s – proceeding", file=sys.stderr)

                print("Looking for Form 1040 PDF link and triggering download…", file=sys.stderr)
                await _click_and_await_download(
                    page,
                    client,
                    lambda: page.click('a[href*="f1040.pdf"]'),
                )

                # Fetch again to get the newly downloaded file
                if cdp_url:
                    await _fetch_remote_downloads(cdp_url)

            except PlaywrightTimeoutError:
                print("Navigation to IRS Forms & Instructions timed out", file=sys.stderr)
                await page.screenshot(path="screenshot-irs-error.png", full_page=True)
            except Exception as e:
                print(f"Error during IRS download test: {e}", file=sys.stderr)
                await page.screenshot(path="screenshot-irs-error.png", full_page=True)
            
        except PlaywrightTimeoutError:
            print(f"Navigation to download test page timed out", file=sys.stderr)
            await page.screenshot(path="screenshot-download-error.png", full_page=True)
        except Exception as e:
            print(f"Error during download test: {e}", file=sys.stderr)
            await page.screenshot(path="screenshot-download-error.png", full_page=True)

        # --------------- Upload test --------------- #
        print(f"Navigating to upload test page...", file=sys.stderr)
        try:
            await page.goto("https://browser-tests-alpha.vercel.app/api/upload-test", wait_until="domcontentloaded", timeout=30_000)
            try:
                await page.wait_for_load_state("networkidle", timeout=10_000)
            except PlaywrightTimeoutError:
                print("networkidle state not reached within 10 s – proceeding", file=sys.stderr)

            # Determine file path to upload
            local_file_path = Path("downloads") / "sandstorm.mp3"
            if not local_file_path.exists():
                raise FileNotFoundError(f"File {local_file_path} not found")

            print(f"Uploading {local_file_path} …", file=sys.stderr)
            file_input = page.locator("#fileUpload")
            await file_input.set_input_files(str(local_file_path))
            print("Upload completed", file=sys.stderr)

            # --------------- Second upload using CDP (remote path) --------------- #
            print("Navigating to upload test page for CDP upload...", file=sys.stderr)
            try:
                await page.goto("https://browser-tests-alpha.vercel.app/api/upload-test", wait_until="domcontentloaded", timeout=30_000)
                try:
                    await page.wait_for_load_state("networkidle", timeout=10_000)
                except PlaywrightTimeoutError:
                    print("networkidle state not reached within 10 s – proceeding", file=sys.stderr)

                # Use CDP commands to set the remote file path in the file input
                root = await client.send("DOM.getDocument", {"depth": 1, "pierce": True})
                input_node = await client.send("DOM.querySelector", {
                    "nodeId": root["root"]["nodeId"],
                    "selector": "#fileUpload"
                })

                remote_file_path = "/tmp/downloads/f1040.pdf"
                await client.send("DOM.setFileInputFiles", {
                    "files": [remote_file_path],
                    "nodeId": input_node["nodeId"]
                })

                # Wait until the file input's value is non-empty
                await page.wait_for_function("document.querySelector('#fileUpload').value.includes('f1040.pdf')", timeout=10_000)
                uploaded_value = await page.evaluate("document.querySelector('#fileUpload').value")
                print(f"CDP upload completed, input value: {uploaded_value}", file=sys.stderr)

            except PlaywrightTimeoutError:
                print("Navigation to upload test page (CDP) timed out", file=sys.stderr)
                await page.screenshot(path="screenshot-upload-cdp-error.png", full_page=True)
            except Exception as e:
                print(f"Error during CDP upload test: {e}", file=sys.stderr)
                await page.screenshot(path="screenshot-upload-cdp-error.png", full_page=True)

        except PlaywrightTimeoutError:
            print("Navigation to upload test page timed out", file=sys.stderr)
            await page.screenshot(path="screenshot-upload-error.png", full_page=True)
        except Exception as e:
            print(f"Error during upload test: {e}", file=sys.stderr)
            await page.screenshot(path="screenshot-upload-error.png", full_page=True)

        await browser.close()


# ---------------- CLI entrypoint ---------------- #

def _resolve_cdp_url(arg: str) -> str:
    """Resolve the provided argument to a CDP websocket URL.

    Treat arg as a url, fetch <url>:9222/json/version, and extract the
    'webSocketDebuggerUrl'.
    """

    # Ensure scheme. Default to http:// if none supplied.
    if not arg.startswith(("http://", "https://")):
        raise ValueError(f"Expected a url, got {arg}")

    version_url = urljoin(arg.rstrip("/") + ":9222/", "json/version")
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


async def _watch_filesystem(endpoint: str) -> None:
    """Watch the filesystem and print events."""
    parsed = urlparse(endpoint)
    fs_api_url = f"https://{parsed.hostname}:444"
    print(f"Filesystem API URL: {fs_api_url}", file=sys.stderr)
    
    watch_id: Optional[str] = None
    
    async with aiohttp.ClientSession() as session:
        try:
            # Ensure /tmp/downloads exists on the remote filesystem
            print(f"Ensuring /tmp/downloads exists via {fs_api_url}/fs/create_directory", file=sys.stderr)
            async with session.put(
                f"{fs_api_url}/fs/create_directory",
                json={"path": "/tmp/downloads", "recursive": True},
            ) as resp:
                if resp.status != 201:
                    text = await resp.text()
                    print(f"Failed to create directory /tmp/downloads: {resp.status} - {text}", file=sys.stderr)

            # Start watching the root directory
            print(f"Starting filesystem watch on /tmp/downloads at {fs_api_url}/fs/watch", file=sys.stderr)
            async with session.put(
                f"{fs_api_url}/fs/watch",
                json={"path": "/tmp/downloads", "recursive": True}
            ) as resp:
                if resp.status != 201:
                    text = await resp.text()
                    print(f"Failed to start filesystem watch: {resp.status} - {text}", file=sys.stderr)
                    return
                
                data = await resp.json()
                watch_id = data.get("watch_id")
                print(f"Filesystem watch started with ID: {watch_id}", file=sys.stderr)
            
            # Stream events
            print(f"Streaming filesystem events from {fs_api_url}/fs/watch/{watch_id}/events", file=sys.stderr)
            async with session.get(
                f"{fs_api_url}/fs/watch/{watch_id}/events",
                headers={"Accept": "text/event-stream"}
            ) as resp:
                if resp.status != 200:
                    text = await resp.text()
                    print(f"Failed to stream filesystem events: {resp.status} - {text}", file=sys.stderr)
                    return
                
                # Process SSE stream
                async for line in resp.content:
                    line_str = line.decode('utf-8').strip()
                    if line_str.startswith("data: "):
                        try:
                            event_data = json.loads(line_str[6:])  # Skip "data: " prefix
                            print(f"FS Event: {event_data}", file=sys.stderr)
                        except json.JSONDecodeError:
                            print(f"Failed to parse event: {line_str}", file=sys.stderr)
                    
        except Exception as exc:
            print(f"Filesystem watch error: {exc}", file=sys.stderr)
        finally:
            # Clean up the watch if it was created
            if watch_id:
                try:
                    print(f"Cleaning up filesystem watch {watch_id}", file=sys.stderr)
                    async with session.delete(f"{fs_api_url}/fs/watch/{watch_id}") as resp:
                        if resp.status == 204 or resp.status == 200:
                            print(f"Filesystem watch {watch_id} cleaned up successfully", file=sys.stderr)
                        else:
                            text = await resp.text()
                            print(f"Failed to clean up watch: {resp.status} - {text}", file=sys.stderr)
                except Exception as cleanup_exc:
                    print(f"Error during watch cleanup: {cleanup_exc}", file=sys.stderr)


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
        
        # Start the filesystem watch task
        fs_watch_task = asyncio.create_task(_watch_filesystem(endpoint_arg))
        
        try:
            await run(cdp_url)
        finally:
            # Ensure the keep-alive task is cancelled cleanly when run() completes.
            keep_alive_task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await keep_alive_task
            
            # Cancel the filesystem watch task
            fs_watch_task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await fs_watch_task

def main() -> None:
    parser = argparse.ArgumentParser(
        description="Browser automation script with CDP or local Chromium",
        epilog="Examples:\n"
               "  CDP mode:   python main.py localhost\n"
               "           or python main.py https://url-of-remote-instance.com"
               "  Local mode: python main.py --local",
        formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("endpoint", nargs="?", help="HTTP endpoint (e.g., localhost). Assumed to be running the devtools protocol on 9222 and the filesystem API on 444.")
    parser.add_argument("--local", action="store_true", help="Launch Chromium locally with GUI instead of connecting via CDP")
    args = parser.parse_args()
    
    if not args.local and not args.endpoint:
        parser.error("Either provide an endpoint or use --local flag")
    
    asyncio.run(_async_main(args.endpoint, args.local))

if __name__ == "__main__":
    main()
