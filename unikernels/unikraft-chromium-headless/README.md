# Headless Chromium x Docker / Unikernel

## Docker

1. Build the image, tagging it with a name you'd like to use:

```bash
IMAGE=chromium-headless
./build-docker.sh
```

2. Run the image

```bash
./run-docker.sh
```

3. Run the test script (from the root of the repo):

```bash
cd shared/cdp-test
uv venv
source .venv/bin/activate
uv sync
uv run python main.py http://localhost:9222
```


Set UKC_TOKEN and UKC_METRO and `./deploy.sh`.

## Test it

```
uv run python cdp-screenshot.py https://<url returned after deploy.sh> https://news.ycombinator.com screenshot.png
```
