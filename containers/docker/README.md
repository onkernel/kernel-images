# 🐋 Chromium x Docker

This Dockerfile extends Anthropic's [Computer Use reference implementation](https://github.com/anthropics/anthropic-quickstarts/tree/main/computer-use-demo) by: (1) installing headful Chromium (2) Exposing Chromium's port `9222` so Chrome DevTools Protocol-based frameworks (Playwright, Puppeteer) can connect to it.

## 1. Build From the Source

```bash
git clone https://github.com/onkernel/kernel-images.git
cd kernel-images
docker build -t kernel-chromium -f containers/docker/Dockerfile .
```

## 2. Run the Container

```bash
docker run -p 8501:8501 -p 8080:8080 -p 6080:6080 -p 9222:9222 kernel-chromium
```

This exposes three ports:

- `8080`: Anthropic's Computer Use web application, which includes a chat interface and remote GUI
- `6080`: NoVNC interface for visual monitoring via browser-based VNC client
- `9222`: Chrome DevTools Protocol for browser automation via Playwright and Puppeteer
- `8501`: Streamlit interfaced used by Computer Use

## 👾 Connect via Chrome DevTools Protocol

We expose port `9222` via ncat, allowing you to connect Chrome DevTools Protocol-based browser frameworks like Playwright and Puppeteer (and CDP-based SDKs like Browser Use). You can use these frameworks to drive the browser in the cloud. 

First, fetch the browser's CDP websocket endpoint:

```typescript
const url = "http://localhost:9222/json/version";
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
const browser = await chromium.connectOverCDP(webSocketDebuggerUrl);
```

## 🧑‍💻 Connect via remote GUI (noVNC)

For visual monitoring, access the browser via NoVNC by opening:

```bash
http://localhost:6080/vnc.html
```

## 🛜 Connect via Anthropic Computer Use's web app

For a unified interface that includes Anthropic Computer Use's chat (via Streamlit) plus GUI (via noVNC), visit:

```bash
http://localhost:8080
```

## 🤝 License & Contributing
See [here](/README.md) for license and contributing details.