<p align="center">
  <img src="static/images/Kernel-Wordmark_Accent.svg" alt="Kernel Logo" width="55%">
  <br/><br/>
  Deployment-ready browsers. Run 'em anywhere
  <br/><br/>
</p>

<p align="center">
  <img alt="GitHub License" src="https://img.shields.io/github/license/onkernel/kernel-images">
  <a href="https://discord.gg/FBrveQRcud"><img src="https://img.shields.io/discord/1342243238748225556?logo=discord&logoColor=white&color=7289DA" alt="Discord"></a>
  <a href="https://x.com/juecd__"><img src="https://img.shields.io/twitter/follow/juecd__" alt="Follow @juecd__"></a>
  <a href="https://x.com/rfgarcia"><img src="https://img.shields.io/twitter/follow/rfgarcia" alt="Follow @rfgarcia"></a>
</p>

## 📜 Table of Contents
- [Overview](#overview)
- [Key Features](#key-features)
- [What You Can Do With It](#what-you-can-do-with-it)
- [Quickstarts](#quickstarts)
- [Contributing](#contributing)
- [License](#license)
- [Support](#support)

## 🤙 Overview

Kernel provides sandboxed, ready-to-use Chrome browser environments for agentic workflows that need to access the Internet. `containers/docker/Dockerfile` and `unikernels/unikraft-cu` are the core infra that powers our hosted services.

🌟[__Sign-up for the waitlist__](https://onkernel.com)🌟

![Chromium x Unikernel Demo](/static/images/unikernel-gh.gif)

### *️⃣ Key Features

- Pre-configured Chrome browser that Chrome DevTools-based browser frameworks (Playwright, Puppeteer) can connect to
- GUI access for visual monitoring and remote control
- Anthropic's [Computer Use](https://github.com/anthropics/anthropic-quickstarts/tree/main/computer-use-demo) agent loop & chat interface baked in

### 💡 What You Can Do With It

- Run automated browser-based workflows
- Develop and test AI agents that use browsers
- Build custom tools that require controlled browser environments

### 🛜 Implementation

`containers/docker` and `unikernels/unikraft-cu` functionally do the same thing:
1. Pull from Anthropic's Computer Use reference implementation
2. Install Chromium
3. Expose ports so Chrome DevTools-based frameworks (Playwright, Puppeteer) can connect to the instance
4. Expose a remote GUI through noVNC

The unikernel implementation works the same as the Docker-only image but has the additional benefits of running on a unikernel: 
- Automated standby / "sleep mode" when there isn't any network activity (consuming negligible resources when it does)
- When it goes into standby mode, the unikernel’s state gets snapshotted and can be restored exactly as it was when it went to sleep. This could be useful if you want to reuse a session’s state (browser auth cookies, interact with local files, browser settings, even the exact page and window zoom you were on).
- Extremely fast cold restarts (<20ms), which could be useful for any application that requires super low latency event handlers.

## 🚀 Quickstarts

- [Unikernel ✨](./unikernels/unikraft-cu/README.md)
- [Docker](./containers/docker/README.md)

## 🧑‍💻 Demo

https://github.com/user-attachments/assets/c62b7ff8-9ba0-4463-9df4-e0dd7f1292ba

## 🤝 Contributing

We welcome contributions to improve this example or add new ones! Please read our [contribution guidelines](./CONTRIBUTING.md) before submitting pull requests.

## 🫰 License

See the [LICENSE](./LICENSE) file for details.

## 🏄 Support

For issues, questions, or feedback, please [open an issue](https://github.com/onkernel/kernel-images/issues) on this repository.

To learn more about our hosted services, [join our waitlist](https://onkernel.com) and our [Discord](https://discord.gg/FBrveQRcud).

![Oprah everyone gets a browser](static/images/oprah-everyone-gets-a-browser.gif)
