
# *️⃣ Chromium x Unikernel

This deploys headful Chromium on a unikernel. It also exposes a remote GUI through noVNC so you can see and use the unikernel's live browser. This unikernel implementation can only be run on Unikraft Cloud, which requires an account. [Join our waitlist](https://onkernel.com) if you don't want to deploy / manage unikernel instances yourself.

![Chromium x Unikernel Demo](/static/images/unikernel-gh.gif)

### 1. Install the Kraft CLI
`curl -sSfL https://get.kraftkit.sh | sh`

### 2. Add Unikraft Secret to Your CLI
`export UKC_METRO=<region> and UKC_TOKEN=<secret>`

### 3. Deploy the Implementation
`./deploy.sh`

When the deployment finishes successfully, the Kraft CLI will print out something like this:
```
Deployed successfully!
 │
 ├───────── name: kernel-cu
 ├───────── uuid: 0cddb958...
 ├──────── metro: <region>
 ├──────── state: starting
 ├─────── domain: https://<service_name>.kraft.host
 ├──────── image: onkernel/kernel-cu@sha256:8265f3f188...
 ├─────── memory: 8192 MiB
 ├────── service: <service_name>
 ├─ private fqdn: <id>
 ├─── private ip: <ip>
 └───────── args: /wrapper.sh
```

### 🧑‍💻 Connect via remote GUI (noVNC)

This implementation maps a noVNC remote GUI to the host port. You can access it by visiting the `domain` listed in Kraft's CLI output above. The remote GUI supports both read and write actions on the browser.

### 👾 Connect via Chrome DevTools Protocol

We expose port `9222` via ncat, allowing you to connect Chrome DevTools Protocol-based browser frameworks like Playwright and Puppeteer (and CDP-based SDKs like Browser Use). You can use these frameworks to drive the browser in the cloud. You can also disconnect from the browser and reconnect to it. The unikernel's browser persists and goes into standby mode when you're not using it.

First, fetch the browser's CDP websocket endpoint:

```typescript
// Use the url provided by the Unikraft deployment
const url = new URL("https://<service_name>.kraft.host:9222/json/version");
const response = await fetch(url, {
  headers: {
    "Host": "<this can be anything>"
  }
});
if (response.status !== 200) {
  throw new Error(
    `Failed to retrieve browser instance: ${
      response.statusText
    } ${await response.text()}`
  );
}
// webSocketDebuggerUrl should look like:
// ws:///devtools/browser/06acd5ef-9961-431d-b6a0-86b99734f816
const { webSocketDebuggerUrl } = await response.json();

// Remove the webSocketDebuggerUrl's ws:// prefix
const webSocketPath = webSocketDebuggerUrl.replace('ws://', '');
// Output will be something like:
// wss://<service_name>.kraft.host:9222/devtools/browser/06acd5ef-9961-431d-b6a0-86b99734f816
const finalWSUrl = `wss://${url.host}${webSocketPath}`;
console.log(finalWSUrl);
```

Then, connect a remote Playwright or Puppeteer client to it:

```typescript
const browser = await puppeteer.connect({
  browserWSEndpoint: finalWSUrl,
});
```

or:

```typescript
const browser = await chromium.connectOverCDP(finalWSUrl);
```

### 📦 Unikernel Notes

- The image requires at least 8gb of memory.
- Various services (mutter, tint) take a few seconds to start-up. Once they do, the standby and restart time is extremely fast. If you'd find a variant of this image useful, message us on [Discord](https://discord.gg/FBrveQRcud)!
- The Unikraft deployment generates a url. This url is public, meaning _anyone_ can access the remote GUI if they have the url. Only use this for non-sensitive browser interactions, and delete the unikernel instance when you're done.
- This deployment doesn't expose the ports to Anthropic's Computer Use's [other interfaces](https://github.com/anthropics/anthropic-quickstarts/tree/main/computer-use-demo#accessing-the-demo-app), but you can do so by altering [deploy.sh](./deploy.sh).
- We're still exploring the limitations of putting a browser on a unikernel. Everything described in this README is from our own observations. If you notice any interesting behavior or limitations of Chromium on a unikernel, please share it on our [Discord](https://discord.gg/FBrveQRcud).
- You can call `browser.close()` to disconnect to the browser, and the unikernel will go into standby after network activity ends. You can then reconnect to the instance using CDP. `browser.close()` ends the websocket connection but doesn't actually close the browser.
- See this repo's [homepage](/README.md) for some benefits of putting Chromium on a unikernel.

### 🤝 License & Contributing
See [here](/README.md) for license and contributing details.