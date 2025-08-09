#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { existsSync } from 'node:fs';
import { readdir } from 'node:fs/promises';
import chalk from 'chalk'

const __dirname = dirname(fileURLToPath(import.meta.url));

;

/**
 * Lists all available test files in the tests directory
 * @returns {Promise<string[]>} Array of test file paths
 */
async function listAvailableTests() {
  try {
    const testsDir = join(__dirname, 'tests');
    const files = await readdir(testsDir);
    return files
      .filter(file => file.endsWith('.test.js'))
      .sort((a, b) => a.localeCompare(b));
  } catch (error) {
    console.error(chalk.red(`Error listing test files: ${error.message}`));
    return [];
  }
}
/**
 * Prints available tests in a formatted way
 */
async function printAvailableTests() {
  const tests = await listAvailableTests();
  
  if (tests.length === 0) {
    console.log(chalk.yellow('No test files found in the tests directory.'));
    return;
  }
  
  console.log(chalk.bold.blue('\n📋 Available Tests:'));
  console.log(chalk.gray('─'.repeat(50)));
  
  tests.forEach((test, index) => {
    const testName = test.replace('.test.js', '');
    console.log(`${chalk.green(`[${index + 1}]`)} ${chalk.white(testName)}`);
  });
  
  console.log(chalk.gray('─'.repeat(50)));
  console.log(chalk.italic(`Run a specific test with: ${chalk.cyan('bun test.js <test-name>')}\n`));
  
  // Add a final separator before tests start
  console.log(chalk.gray('═'.repeat(70)));
  console.log(chalk.bold.green('▶ Starting Tests...'));
  console.log(chalk.gray('═'.repeat(70)) + '\n');
}

printAvailableTests()

// Parse command line arguments
const args = process.argv.slice(2);
const testFiles = [];
const vitestArgs = [];

// Process arguments
args.forEach(arg => {
  if (arg.startsWith('--') || arg.startsWith('-')) {
    vitestArgs.push(arg);
  } else {
    testFiles.push(arg);
  }
});

// If no test files specified, run all tests
if (testFiles.length === 0) {
  console.log('Running all tests...');
} else {
  console.log(`Running tests: ${testFiles.join(', ')}`);
}

// Prepare test file paths
const testPaths = testFiles.map(file => {
  // If file doesn't end with .test.js, add it
  const testFile = file.endsWith('.test.js') ? file : `${file}.test.js`;
  return join(__dirname, 'tests', testFile);
}).filter(path => {
  const exists = existsSync(path);
  if (!exists) {
    console.warn(`Warning: Test file not found: ${path}`);
  }
  return exists;
});

// Build vitest command
const command = 'npx';
const commandArgs = [
  'vitest',
  'run',
  ...(testPaths.length > 0 ? testPaths : []),
  ...vitestArgs
];

// Run the tests
const testProcess = spawn(command, commandArgs, {
  stdio: 'inherit',
  shell: true,
  env: {
    ...process.env,
    PORT: '9999' // Ensure PORT is set in the environment for BASE_URL in tests
  }
});

testProcess.on('close', code => {
  process.exit(code);
});

/*
Examples of how to use this test runner:

1. Run all tests:
   bun test.js

2. Run a specific test file:
   bun test.js screenshot

3. Run multiple test files:
   bun test.js screenshot stream

4. Run with vitest options:
   bun test.js screenshot --watch

5. Run with both file and options:
   bun test.js screenshot --bail
*/
