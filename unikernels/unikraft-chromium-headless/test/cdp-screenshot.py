import sys
from playwright.sync_api import sync_playwright

if len(sys.argv) != 4:
    print(f"Usage: {sys.argv[0]} <cdp-url> <url-to-screenshot> <screenshot_filename>", file=sys.stderr)
    print(f"Example: {sys.argv[0]} https://cdp-chromium.sfo0-tinyfish.unikraft.app https://google.com 1.png", file=sys.stderr)
    sys.exit(1)

cdp_url = sys.argv[1]
screenshot_url = sys.argv[2]
screenshot_filename = sys.argv[3]

p = sync_playwright().start()

browser = p.chromium.connect_over_cdp(cdp_url)

# Open a new browser page.
USER_AGENT = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4.1 Safari/605.1.15"
new_page = browser.new_page(user_agent=USER_AGENT)
new_page.set_extra_http_headers(
        {"sec-ch-ua": '"Chromium";v="125", "Not.A/Brand";v="24"'}
    )
new_page.goto(screenshot_url)

MAX_SCREENSHOT_HEIGHT = 16384
dimensions = new_page.evaluate(
    """
    () => {
        return {
           width: document.documentElement.scrollWidth,
            height: document.documentElement.scrollHeight,
            deviceScaleFactor: window.devicePixelRatio,
        }
    }
"""
)

# Set the viewport to the full page size (up to a maximum height of MAX_ALLOWED_HEIGHT)
new_page.set_viewport_size(
        {
            "width": dimensions["width"],
            "height": min(dimensions["height"], MAX_SCREENSHOT_HEIGHT),
        }
    )

# Take a single screenshot of the entire page
screenshot = new_page.screenshot()

with open(screenshot_filename, "wb") as stream:
    stream.write(screenshot)
