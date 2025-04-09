# Kernel Containers

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

## Quickstart

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
const response = await fetch("http://localhost:9222/json/version");
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

```
http://localhost:6080/vnc.html
```

### Via Anthropic Computer Use's Web Application

For a unified interface that includes Anthropic Computer Use's chat (via Streamlit) plus GUI (via noVNC), visit:

```
http://localhost:8080
```

## Contributing

We welcome contributions to improve this example or add new ones! Please read our [contribution guidelines](./CONTRIBUTING.md) before submitting pull requests.

## License

See the [LICENSE](./LICENSE) file for details.

## Support

For issues, questions, or feedback, please [open an issue](https://github.com/onkernel/kernel-containers/issues) on this repository.

To learn more about our hosted services, visit [our docs](https://docs.onkernel.com/introduction) and request an API key on [Discord](https://discord.gg/FBrveQRcud).
