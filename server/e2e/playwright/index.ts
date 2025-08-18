#!/usr/bin/env tsx

import { writeFileSync } from 'fs';
import { Browser, BrowserContext, chromium, Page } from 'playwright-core';

interface CommandOptions {
  wsURL?: string;
  timeout?: number;
}

interface NavigateCookieOptions extends CommandOptions {
  url: string;
  cookieName: string;
  cookieValue: string;
  label?: string;
}

interface NavigateCookieFormOptions extends CommandOptions {
  url: string;
  cookieName: string;
  cookieValue: string;
  label?: string;
}

interface LocalStorageOptions extends CommandOptions {
  url: string;
  key: string;
  value: string;
  label?: string;
}

interface HistoryOptions extends CommandOptions {
  urls: string[];
  label?: string;
}

interface NavigateXAndBackOptions extends CommandOptions {
  label?: string;
}

interface ScreenshotOptions extends CommandOptions {
  filename: string;
}

class CDPClient {
  private browser?: Browser;
  private context?: BrowserContext;
  private page?: Page;

  async connect(wsURL: string = 'ws://127.0.0.1:9222/'): Promise<void> {
    try {
      // Connect to existing browser via CDP
      this.browser = await chromium.connectOverCDP(wsURL);

      // Get the default context (or first available context)
      const contexts = this.browser.contexts();
      if (contexts.length > 0) {
        this.context = contexts[0];
      } else {
        // This shouldn't happen with an existing browser, but just in case
        this.context = await this.browser.newContext();
      }

      // Get existing page or create new one
      const pages = this.context.pages();
      if (pages.length > 0) {
        this.page = pages[0];
      } else {
        this.page = await this.context.newPage();
      }
    } catch (error) {
      console.error('Failed to connect to browser:', error);
      throw error;
    }
  }

  async navigateAndEnsureCookie(options: NavigateCookieOptions): Promise<void> {
    if (!this.page) throw new Error('Not connected to browser');

    const { url, cookieName, cookieValue, label = 'check', timeout = 45000 } = options;

    // Array to collect browser console logs
    const browserLogs: string[] = [];
    // Handler to push logs from browser console
    const consoleListener = (msg: any) => {
      // Only log 'log', 'warn', 'error', 'info' types
      if (['log', 'warn', 'error', 'info'].includes(msg.type())) {
        // Join all arguments as string
        const text = msg.text();
        browserLogs.push(`[browser][${msg.type()}] ${text}`);
      }
    };

    try {
      console.log(`[cdp] action: navigate-cookie, url: ${url}, label: ${label}`);

      // Attach console listener
      this.page.on('console', consoleListener);

      // Set timeout for this operation
      this.page.setDefaultTimeout(timeout);

      // Navigate to the URL
      await this.page.goto(url, { waitUntil: 'domcontentloaded' });

      // Wait for #cookies element to be visible
      await this.page.waitForSelector('#cookies', { state: 'visible', timeout: 5000 });

      // Get the text content of #cookies element
      const cookiesText = await this.page.textContent('#cookies');

      // Echo browser console logs
      if (browserLogs.length > 0) {
        for (const log of browserLogs) {
          console.log(log);
        }
      }

      if (!cookiesText) {
        throw new Error('#cookies element has no text content');
      }

      // Check if the cookie exists with the expected value
      const expectedCookie = `${cookieName}=${cookieValue}`;
      if (!cookiesText.includes(expectedCookie)) {
        // Take a screenshot on failure
        const screenshotPath = `cookie-verify-miss-${label}.png`;
        await this.captureScreenshot({ filename: screenshotPath });
        throw new Error(`Expected document.cookie to contain "${expectedCookie}", got "${cookiesText}"`);
      }

      console.log(`Cookie verified successfully: ${cookieName}=${cookieValue}`);

    } catch (error) {
      // Echo browser console logs on error as well
      if (browserLogs.length > 0) {
        for (const log of browserLogs) {
          console.log(log);
        }
      }
      // Take a screenshot on any error
      const screenshotPath = `cookie-verify-${label}.png`;
      await this.captureScreenshot({ filename: screenshotPath }).catch(console.error);
      throw error;
    } finally {
      // Remove the console listener to avoid leaks
      if (this.page) {
        this.page.off('console', consoleListener);
      }
    }
  }

  async setAndVerifyLocalStorage(options: LocalStorageOptions): Promise<void> {
    if (!this.page) throw new Error('Not connected to browser');

    const { url, key, value, label = 'localstorage', timeout = 45000 } = options;

    try {
      console.log(`[cdp] action: set-localstorage, url: ${url}, key: ${key}, value: ${value}, label: ${label}`);

      // Set timeout for this operation
      this.page.setDefaultTimeout(timeout);

      // Navigate to the URL
      await this.page.goto(url, { waitUntil: 'domcontentloaded' });

      // Set localStorage value
      await this.page.evaluate(({ k, v }: { k: string; v: string }) => {
        (globalThis as any).localStorage.setItem(k, v);
        console.log(`[localStorage] Set ${k}=${v}`);
      }, { k: key, v: value });

      // Verify localStorage value
      const storedValue = await this.page.evaluate(({ k }: { k: string }) => {
        return (globalThis as any).localStorage.getItem(k);
      }, { k: key });

      if (storedValue !== value) {
        const screenshotPath = `localstorage-verify-miss-${label}.png`;
        await this.captureScreenshot({ filename: screenshotPath });
        throw new Error(`Expected localStorage["${key}"] to be "${value}", got "${storedValue}"`);
      }

      console.log(`LocalStorage verified successfully: ${key}=${value}`);

      // Navigate to google.com to potentially force a flush
      console.log('[cdp] action: navigating to google.com to force localStorage flush');
      try {
        await this.page.goto('https://www.google.com', { waitUntil: 'domcontentloaded' });
        console.log('[cdp] action: google.com navigation completed');
      } catch (navError) {
        console.warn('[cdp] action: google.com navigation failed, continuing anyway:', navError);
      }
    } catch (error) {
      const screenshotPath = `localstorage-verify-${label}.png`;
      await this.captureScreenshot({ filename: screenshotPath }).catch(console.error);
      throw error;
    }
  }

  async verifyLocalStorage(options: LocalStorageOptions): Promise<void> {
    if (!this.page) throw new Error('Not connected to browser');

    const { url, key, value, label = 'localstorage-verify', timeout = 45000 } = options;

    try {
      console.log(`[cdp] action: verify-localstorage, url: ${url}, key: ${key}, expected: ${value}, label: ${label}`);

      // Set timeout for this operation
      this.page.setDefaultTimeout(timeout);

      // Navigate to the URL
      await this.page.goto(url, { waitUntil: 'domcontentloaded' });

      // Get localStorage value
      const storedValue = await this.page.evaluate(({ k }: { k: string }) => {
        return (globalThis as any).localStorage.getItem(k);
      }, { k: key });

      if (storedValue !== value) {
        const screenshotPath = `localstorage-verify-fail-${label}.png`;
        await this.captureScreenshot({ filename: screenshotPath });
        throw new Error(`Expected localStorage["${key}"] to be "${value}", got "${storedValue}"`);
      }

      console.log(`LocalStorage verification successful: ${key}=${value}`);
    } catch (error) {
      const screenshotPath = `localstorage-verify-fail-${label}.png`;
      await this.captureScreenshot({ filename: screenshotPath }).catch(console.error);
      throw error;
    }
  }

  async navigateToXAndBack(options: NavigateXAndBackOptions): Promise<void> {
    if (!this.page) throw new Error('Not connected to browser');

    const { label = 'x-navigation', timeout = 45000 } = options;

    try {
      console.log(`[cdp] action: navigate-to-x-and-back, label: ${label}`);

      // Set timeout for this operation
      this.page.setDefaultTimeout(timeout);

      // Do the navigation to x.com and back twice in a loop
      for (let i = 0; i < 2; i++) {
        console.log(`[cdp] action: [${i + 1}/2] navigating to x.com`);
        await this.page.goto('https://x.com', { waitUntil: 'domcontentloaded' });

        // Wait a bit to ensure cookies are set
        await this.page.waitForTimeout(2000);

        console.log(`[cdp] action: [${i + 1}/2] navigating to news.ycombinator.com`);
        await this.page.goto('https://news.ycombinator.com', { waitUntil: 'domcontentloaded' });

        // Wait a bit to ensure the navigation is recorded
        await this.page.waitForTimeout(2000);
      }

      console.log('X.com navigation and return completed successfully');

    } catch (error) {
      const screenshotPath = `x-navigation-${label}.png`;
      await this.captureScreenshot({ filename: screenshotPath }).catch(console.error);
      throw error;
    }
  }

  async captureScreenshot(options: ScreenshotOptions): Promise<void> {
    if (!this.page) throw new Error('Not connected to browser');

    const { filename } = options;

    try {
      // Take a full page screenshot
      const screenshot = await this.page.screenshot({
        fullPage: true,
        type: 'png',
      });

      // Write to file
      writeFileSync(filename, screenshot);
      console.log(`Screenshot saved to: ${filename}`);
    } catch (error) {
      console.error('Failed to capture screenshot:', error);
      throw error;
    }
  }

  async disconnect(): Promise<void> {
    // Note: We don't close the browser since it's an existing instance
    // We just disconnect from it
    if (this.browser) {
      await this.browser.close().catch(() => {
        // Ignore errors when disconnecting
      });
    }
  }
}

async function main(): Promise<void> {
  const args = process.argv.slice(2);

  if (args.length === 0) {
    console.error('Usage: tsx index.ts <command> [options]');
    console.error('Commands:');
    console.error('  navigate-and-ensure-cookie --url <url> --cookie-name <name> --cookie-value <value> [--label <label>] [--ws-url <ws>] [--timeout <ms>]');
    console.error('  set-localstorage --url <url> --key <key> --value <value> [--label <label>] [--ws-url <ws>] [--timeout <ms>]');
    console.error('  verify-localstorage --url <url> --key <key> --value <value> [--label <label>] [--ws-url <ws>] [--timeout <ms>]');
    console.error('  navigate-to-x-and-back [--label <label>] [--ws-url <ws>] [--timeout <ms>]');
    process.exit(1);
  }

  const command = args[0];
  const options: Record<string, string> = {};

  // Parse command line arguments
  for (let i = 1; i < args.length; i += 2) {
    const key = args[i];
    const value = args[i + 1];
    if (key.startsWith('--') && value) {
      options[key.substring(2)] = value;
    }
  }

  const client = new CDPClient();

  try {
    // Connect to browser
    const wsURL = options['ws-url'] || 'ws://127.0.0.1:9222/';
    await client.connect(wsURL);

    switch (command) {
      case 'navigate-and-ensure-cookie': {
        if (!options.url || !options['cookie-name'] || !options['cookie-value']) {
          throw new Error('Missing required options: --url, --cookie-name, --cookie-value');
        }

        await client.navigateAndEnsureCookie({
          wsURL,
          url: options.url,
          cookieName: options['cookie-name'],
          cookieValue: options['cookie-value'],
          label: options.label,
          timeout: options.timeout ? parseInt(options.timeout, 10) : undefined,
        });
        break;
      }

      case 'set-localstorage': {
        if (!options.url || !options.key || !options.value) {
          throw new Error('Missing required options: --url, --key, --value');
        }

        await client.setAndVerifyLocalStorage({
          wsURL,
          url: options.url,
          key: options.key,
          value: options.value,
          label: options.label,
          timeout: options.timeout ? parseInt(options.timeout, 10) : undefined,
        });
        break;
      }

      case 'verify-localstorage': {
        if (!options.url || !options.key || !options.value) {
          throw new Error('Missing required options: --url, --key, --value');
        }

        await client.verifyLocalStorage({
          wsURL,
          url: options.url,
          key: options.key,
          value: options.value,
          label: options.label,
          timeout: options.timeout ? parseInt(options.timeout, 10) : undefined,
        });
        break;
      }

      case 'navigate-to-x-and-back': {
        await client.navigateToXAndBack({
          wsURL,
          label: options.label,
          timeout: options.timeout ? parseInt(options.timeout, 10) : undefined,
        });
        break;
      }

      default:
        throw new Error(`Unknown command: ${command}`);
    }

    process.exit(0);
  } catch (error) {
    console.error('Error:', error);
    process.exit(1);
  } finally {
    await client.disconnect();
  }
}

// Run the main function
main().catch((error) => {
  console.error('Unhandled error:', error);
  process.exit(1);
});
