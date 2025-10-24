import { readFileSync } from 'fs';
import { chromium } from 'playwright-core';

async function main() {
  const codeFilePath = process.argv[2];

  if (!codeFilePath) {
    console.error('Usage: tsx playwright-executor.ts <code-file-path>');
    process.exit(1);
  }

  let userCode: string;
  try {
    userCode = readFileSync(codeFilePath, 'utf-8');
  } catch (error: any) {
    console.error(JSON.stringify({
      success: false,
      error: `Failed to read code file: ${error.message}`
    }));
    process.exit(1);
  }

  let browser;
  let result;

  try {
    browser = await chromium.connectOverCDP('ws://127.0.0.1:9222');
    const contexts = browser.contexts();
    const context = contexts.length > 0 ? contexts[0] : await browser.newContext();
    const pages = context.pages();
    const page = pages.length > 0 ? pages[0] : await context.newPage();

    const AsyncFunction = Object.getPrototypeOf(async function () { }).constructor;
    const userFunction = new AsyncFunction('page', 'context', 'browser', userCode);
    result = await userFunction(page, context, browser);

    if (result !== undefined) {
      console.log(JSON.stringify({ success: true, result: result }));
    } else {
      console.log(JSON.stringify({ success: true, result: null }));
    }
  } catch (error: any) {
    console.error(JSON.stringify({
      success: false,
      error: error.message,
      stack: error.stack
    }));
    process.exit(1);
  } finally {
    if (browser) {
      try {
        await browser.close();
      } catch (e) {
        // Ignore errors when closing CDP connection
      }
    }
  }
}

main();
