# Headless Chromium x Docker / Unikernel

## Docker

1. Build the image, tagging it with a name you'd like to use:

```bash
export IMAGE=chromium-headless
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

## Unikernel

1. Build the image, tagging it with a name you'd like to use:

```bash
export IMAGE=chromium-headless
./build-unikernel.sh
```

2. Set UKC_TOKEN and UKC_METRO and `./deploy.sh`.

## Test it

3. Run the test script (from the root of the repo):

```bash
cd shared/cdp-test
uv venv
source .venv/bin/activate
uv sync
uv run python main.py <kraft instance https url>:9222
```

4. Check on memory use. In shared/uk-check-stats.sh there's a script that will poll the `/stats` endpoint of Unikraft Cloud to see RSS (memory) used by the VM. This is good for tailoring the GB resource request when creating an instance.
