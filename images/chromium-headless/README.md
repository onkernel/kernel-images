# Headless Chromium x Docker / Unikernel

## Docker

1. Build and run the image, tagging it with a name you'd like to use:

```bash
./build-docker.sh
./run-docker.sh
```

2. Run the test script (from the root of the repo):

```bash
cd shared/cdp-test
uv venv
source .venv/bin/activate
uv sync
uv run python main.py http://localhost:9222
```

## Unikernel

1. Build and run the image, tagging it with a name you'd like to use:

```bash
export UKC_TOKEN=
export UKC_METRO=
# latest UKC also allows pushing to metro-specific indexes
# e.g. index.<UKC_METRO hostname>
export UKC_INDEX=index.unikraft.io
./build-unikernel.sh
./run-unikernel.sh
```

2. Run the test script (from the root of the repo):

```bash
cd shared/cdp-test
uv venv
source .venv/bin/activate
uv sync
uv run python main.py <kraft instance https url>:9222
```

3. Check on memory use. In `shared/uk-check-stats.sh` there's a script that will poll the `/stats` endpoint of Unikraft Cloud to see RSS (memory) used by the VM. This is good for tailoring the GB resource request when creating an instance.
