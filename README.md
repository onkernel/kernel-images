# Kernel Containers

## Table of Contents
- [Overview](#overview)
- [Key Features](#key-features)
- [What You Can Do With It][#what-you-can-do-with-it]
- [Quickstart - Unikernel](#quickstart-unikernel)
- [Quickstart - Docker](#quickstart-docker)
- [Connecting to the Browser](#connecting-to-the-browser)
- [Contributing](#contributing)
- [License](#license)
- [Support](#support)

## Overview

Kernel provides containerized, ready-to-use Chrome browser environments for agentic workflows that need to access the Internet. `containers/docker/Dockerfile` and `unikernels/unikraft-cu` are the core infra that powers our [hosted services](https://docs.onkernel.com/introduction).

### Key Features

- Pre-configured Chrome browser that Chrome DevTools-based browser frameworks (Playwright, Puppeteer) can connect to
- GUI access for visual monitoring and remote control
- Anthropic's [Computer Use](https://github.com/anthropics/anthropic-quickstarts/tree/main/computer-use-demo) agent loop & chat interface baked in

### What You Can Do With It

- Run automated browser-based workflows
- Develop and test AI agents that need web capabilities
- Build custom tools that require controlled browser environments

`containers/docker` and `unikernels/unikraft-cu` functionally do the same thing: they pull from Anthropic's Computer Use Reference Implementation, install Chromium, and expose ports so Chrome DevTools-based frameworks (Playwright, Puppeteer) can connect to the instance. The unikernel implementation works the same but has the additional benefits of running on a unikernel: automated standby / "sleep mode" when there isn't any network activity (consuming very low resources when it does) and extremely fast restarts (<20ms). This can be useful for browser automations that involve asynchronous processing or scenarios where you want to return to the same session state hours, days, or weeks later.

## Quickstart - Unikernel

Our unikernel implementation can only be run on Unikraft Cloud, which requires an account. Request one [here](https://console.unikraft.cloud/signup).

### 1. Install the Kraft CLI
`curl -sSfL https://get.kraftkit.sh | sh`

### 2. Add Unikraft Secret
`export UKC_METRO=was1 and UKC_TOKEN=<secret>`

### 3. Deploy the Implementation
`./deploy.sh`

Then follow [these steps](#connecting-to-the-browser) to connect to the available ports on the instance.

### Unikernel / Unikraft Notes
- The image requires 8gb of memory on the unikernels. The Unikraft default is 4gb, so request a higher limit with their team.
- Various Computer Use services (mutter, tint) take a few seconds to start-up. Once they do, though, the standby and restart time is extremely fast. If you'd find a variant of this image useful, message us on [Discord](https://discord.gg/FBrveQRcud)!

## Quickstart - Docker

### 1. Build From the Source

```bash
git clone https://github.com/onkernel/kernel-containers.git
cd kernel-containers
docker build -t kernel-chromium -f containers/docker/Dockerfile .
```

### 2. Run the Container

```bash
docker run -p 8501:8501 -p 8080:8080 -p 6080:6080 -p 9222:9222 kernel-chromium
```

This exposes three ports:

- `8080`: Anthropic's Computer Use web application, which includes a chat interface and remote GUI
- `6080`: NoVNC interface for visual monitoring via browser-based VNC client
- `9222`: Chrome DevTools Protocol for browser automation via Playwright and Puppeteer
- `8501`: Streamlit interfaced used by Computer Use

## Connecting to the Browser

### Via Chrome DevTools Protocol

You can connect to the browser using any CDP client.

First, fetch the browser's CDP websocket endpoint:

```typescript
/**
 * Uncomment the relevant url
 * const url = ""; // Unikraft deployment
 * const url = "http://localhost:9222/json/version"; // Local Docker
**/
const response = await fetch(url);
if (response.status !== 200) {
  throw new Error(
    `Failed to retrieve browser instance: ${
      response.statusText
    } ${await response.text()}`
  );
}
const { webSocketDebuggerUrl } = await response.json();
```

Then, connect a remote Playwright or Puppeteer client to it:

```typescript
const browser = await puppeteer.connect({
  browserWSEndpoint: webSocketDebuggerUrl,
});
```

or:

```typescript
browser = await chromium.connectOverCDP(cdp_ws_url);
```

### Via GUI (NoVNC)

For visual monitoring, access the browser via NoVNC by opening:

```bash
# Unikraft deployment
https://XXX/vnc.html

# Local Docker
http://localhost:6080/vnc.html
```

### Via Anthropic Computer Use's Web Application

For a unified interface that includes Anthropic Computer Use's chat (via Streamlit) plus GUI (via noVNC), visit:

```bash
# Unikraft deployment

# Local Docker
http://localhost:8080
```

## Contributing

We welcome contributions to improve this example or add new ones! Please read our [contribution guidelines](./CONTRIBUTING.md) before submitting pull requests.

## License

See the [LICENSE](./LICENSE) file for details.

## Support

For issues, questions, or feedback, please [open an issue](https://github.com/onkernel/kernel-containers/issues) on this repository.

To learn more about our hosted services, visit [our docs](https://docs.onkernel.com/introduction) and request an API key on [Discord](https://discord.gg/FBrveQRcud).
