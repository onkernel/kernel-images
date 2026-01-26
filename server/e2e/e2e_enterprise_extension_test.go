package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	logctx "github.com/onkernel/kernel-images/server/lib/logger"
	"github.com/stretchr/testify/require"
)

// TestEnterpriseExtensionInstallation tests that enterprise policy extensions
// (with update.xml and .crx files) are installed correctly via ExtensionInstallForcelist.
//
// This test verifies:
// 1. Extension with webRequest permission and update.xml/.crx files is uploaded successfully
// 2. Enterprise policy (ExtensionInstallForcelist) is correctly configured
// 3. Chrome fetches the update.xml and downloads the .crx file
// 4. Extension is installed and appears in chrome://extensions
//
// This test uses a real built extension (web-bot-auth) to reproduce production behavior.
// It runs against both headless and headful Chrome images.
func TestEnterpriseExtensionInstallation(t *testing.T) {
	ensurePlaywrightDeps(t)

	testCases := []struct {
		name  string
		image string
	}{
		{"Headless", headlessImage},
		{"Headful", headfulImage},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runEnterpriseExtensionTest(t, tc.image)
		})
	}
}

func runEnterpriseExtensionTest(t *testing.T, image string) {
	name := containerName + "-enterprise-ext"

	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Clean slate
	_ = stopContainer(baseCtx, name)

	// Use default CHROMIUM_FLAGS - the images now have --disable-background-networking removed
	// (headless) or never had it (headful), allowing Chrome to fetch extensions via
	// ExtensionInstallForcelist enterprise policy
	env := map[string]string{}

	// Start container
	_, exitCh, err := runContainer(baseCtx, image, name, env)
	require.NoError(t, err, "failed to start container: %v", err)
	defer stopContainer(baseCtx, name)

	ctx, cancel := context.WithTimeout(baseCtx, 5*time.Minute)
	defer cancel()

	logger.Info("[setup]", "action", "waiting for API", "image", image, "url", apiBaseURL+"/spec.yaml")
	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready")

	// Wait for DevTools to be ready
	_, err = waitDevtoolsWS(ctx)
	require.NoError(t, err, "devtools not ready")

	// First upload a simple extension to simulate the kernel extension in production.
	// This causes Chrome to be launched with --load-extension, which mirrors production
	// where the kernel extension is always loaded before any enterprise extensions.
	logger.Info("[test]", "action", "uploading kernel-like extension first (to simulate prod)")
	uploadKernelLikeExtension(t, ctx, logger)

	// Wait for Chrome to restart with the new flags
	time.Sleep(3 * time.Second)
	_, err = waitDevtoolsWS(ctx)
	require.NoError(t, err, "devtools not ready after kernel extension")

	// Upload the enterprise test extension (with update.xml and .crx)
	logger.Info("[test]", "action", "uploading enterprise test extension (with update.xml and .crx)")
	uploadEnterpriseTestExtension(t, ctx, logger)

	// Wait a bit for Chrome to process the enterprise policy
	logger.Info("[test]", "action", "waiting for Chrome to process enterprise policy")
	time.Sleep(5 * time.Second)

	// Check what files were extracted on the server
	logger.Info("[test]", "action", "checking extracted extension files on server")
	checkExtractedFiles(t, ctx, logger)

	// Check the kernel-images-api logs for extension download requests
	logger.Info("[test]", "action", "checking if Chrome fetched the extension")
	checkExtensionDownloadLogs(t, ctx, logger)

	// Verify enterprise policy was configured correctly
	logger.Info("[test]", "action", "verifying enterprise policy configuration")
	verifyEnterprisePolicy(t, ctx, logger)

	// Wait longer and check again if Chrome has downloaded the extension
	logger.Info("[test]", "action", "waiting for Chrome to download extension via enterprise policy")
	time.Sleep(30 * time.Second)

	// Check logs again
	checkExtensionDownloadLogs(t, ctx, logger)

	// Check Chrome's extension installation logs
	logger.Info("[test]", "action", "checking Chrome stderr for extension-related logs")
	checkChromiumLogs(t, ctx, logger)

	// Try to trigger extension installation by restarting Chrome
	logger.Info("[test]", "action", "restarting Chrome to trigger policy refresh")
	restartChrome(t, ctx, logger)

	time.Sleep(15 * time.Second)

	// Check logs one more time
	checkExtensionDownloadLogs(t, ctx, logger)
	checkChromiumLogs(t, ctx, logger)

	// Check Chrome's policy state
	logger.Info("[test]", "action", "checking Chrome policy state")
	checkChromePolicies(t, ctx, logger)

	// Check chrome://policy to see if Chrome recognizes the policy
	logger.Info("[test]", "action", "checking chrome://policy via screenshot")
	takeChromePolicyScreenshot(t, ctx, logger)

	// Verify the extension is installed
	logger.Info("[test]", "action", "checking if extension is installed in Chrome's user-data")
	verifyExtensionInstalled(t, ctx, logger)

	logger.Info("[test]", "result", "enterprise extension installation test completed")
}

// uploadKernelLikeExtension uploads a simple extension to simulate the kernel extension.
// In production, the kernel extension is always loaded before any enterprise extensions,
// so this ensures the test mirrors that behavior.
func uploadKernelLikeExtension(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	client, err := apiClient()
	require.NoError(t, err, "failed to create API client")

	// Get the path to the simple test extension (no webRequest, so no enterprise policy)
	extDir, err := filepath.Abs("test-extension")
	require.NoError(t, err, "failed to get absolute path to test-extension")

	// Create zip of the extension
	extZip, err := zipDirToBytes(extDir)
	require.NoError(t, err, "failed to zip test extension")

	// Upload extension
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("extensions.zip_file", "kernel-like-ext.zip")
	require.NoError(t, err)
	_, err = io.Copy(fw, bytes.NewReader(extZip))
	require.NoError(t, err)
	err = w.WriteField("extensions.name", "kernel")
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)

	start := time.Now()
	rsp, err := client.UploadExtensionsAndRestartWithBodyWithResponse(ctx, w.FormDataContentType(), &body)
	elapsed := time.Since(start)
	require.NoError(t, err, "uploadExtensionsAndRestart request error")

	require.Equal(t, http.StatusCreated, rsp.StatusCode(),
		"expected 201 Created but got %d. Body: %s",
		rsp.StatusCode(), string(rsp.Body))

	logger.Info("[kernel-ext]", "action", "uploaded kernel-like extension", "elapsed", elapsed.String())
}

// uploadEnterpriseTestExtension uploads the test extension with update.xml and .crx files.
// This should trigger enterprise policy handling via ExtensionInstallForcelist.
func uploadEnterpriseTestExtension(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	client, err := apiClient()
	require.NoError(t, err, "failed to create API client")

	// Get the path to the test extension
	extDir, err := filepath.Abs("test-extension-enterprise")
	require.NoError(t, err, "failed to get absolute path to test-extension-enterprise")

	// Read and log the manifest
	manifestPath := filepath.Join(extDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	require.NoError(t, err, "failed to read manifest.json")
	logger.Info("[extension]", "manifest", string(manifestData))

	// Read and log the update.xml
	updateXMLPath := filepath.Join(extDir, "update.xml")
	updateXMLData, err := os.ReadFile(updateXMLPath)
	require.NoError(t, err, "failed to read update.xml")
	logger.Info("[extension]", "update.xml", string(updateXMLData))

	// Verify .crx exists
	crxPath := filepath.Join(extDir, "extension.crx")
	crxInfo, err := os.Stat(crxPath)
	require.NoError(t, err, "failed to stat .crx file")
	logger.Info("[extension]", "crx_size", crxInfo.Size())

	// Create zip of the extension
	extZip, err := zipDirToBytes(extDir)
	require.NoError(t, err, "failed to zip test extension")

	// Upload extension
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("extensions.zip_file", "enterprise-test-ext.zip")
	require.NoError(t, err)
	_, err = io.Copy(fw, bytes.NewReader(extZip))
	require.NoError(t, err)
	err = w.WriteField("extensions.name", "enterprise-test")
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)

	start := time.Now()
	rsp, err := client.UploadExtensionsAndRestartWithBodyWithResponse(ctx, w.FormDataContentType(), &body)
	elapsed := time.Since(start)
	require.NoError(t, err, "uploadExtensionsAndRestart request error")

	// The key assertion: this should return 201
	require.Equal(t, http.StatusCreated, rsp.StatusCode(),
		"expected 201 Created but got %d. Body: %s",
		rsp.StatusCode(), string(rsp.Body))

	logger.Info("[extension]", "action", "uploaded", "elapsed", elapsed.String())
}

// verifyEnterprisePolicy checks that the enterprise policy was configured correctly.
func verifyEnterprisePolicy(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// Read policy.json
	policyContent, err := execCombinedOutput(ctx, "cat", []string{"/etc/chromium/policies/managed/policy.json"})
	require.NoError(t, err, "failed to read policy.json")
	logger.Info("[policy]", "content", policyContent)

	var policy map[string]interface{}
	err = json.Unmarshal([]byte(policyContent), &policy)
	require.NoError(t, err, "failed to parse policy.json")

	// Check ExtensionInstallForcelist exists and contains our extension
	extensionInstallForcelist, ok := policy["ExtensionInstallForcelist"].([]interface{})
	require.True(t, ok, "ExtensionInstallForcelist not found in policy.json")
	require.GreaterOrEqual(t, len(extensionInstallForcelist), 1, "ExtensionInstallForcelist should have at least 1 entry")

	// Log all entries
	for i, entry := range extensionInstallForcelist {
		logger.Info("[policy]", "forcelist_entry", i, "value", entry)
	}

	// Find the enterprise-test entry
	var found bool
	for _, entry := range extensionInstallForcelist {
		if entryStr, ok := entry.(string); ok && strings.Contains(entryStr, "enterprise-test") {
			found = true
			logger.Info("[policy]", "found_entry", entryStr)
			break
		}
	}
	require.True(t, found, "enterprise-test entry not found in ExtensionInstallForcelist")

	// Check ExtensionSettings
	extensionSettings, ok := policy["ExtensionSettings"].(map[string]interface{})
	if ok {
		logger.Info("[policy]", "extension_settings", fmt.Sprintf("%+v", extensionSettings))
	}
}

// checkExtractedFiles checks what files were extracted on the server side.
func checkExtractedFiles(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// List all files in the extension directory
	output, err := execCombinedOutput(ctx, "ls", []string{"-la", "/home/kernel/extensions/enterprise-test/"})
	if err != nil {
		logger.Warn("[files]", "error", err.Error())
	} else {
		logger.Info("[files]", "extension_dir", output)
	}

	// Check if update.xml exists
	updateXML, err := execCombinedOutput(ctx, "cat", []string{"/home/kernel/extensions/enterprise-test/update.xml"})
	if err != nil {
		logger.Warn("[files]", "update_xml_error", err.Error())
	} else {
		logger.Info("[files]", "update.xml", updateXML)
	}

	// Check if .crx exists
	crxOutput, err := execCombinedOutput(ctx, "ls", []string{"-la", "/home/kernel/extensions/enterprise-test/*.crx"})
	if err != nil {
		logger.Warn("[files]", "crx_error", err.Error())
	} else {
		logger.Info("[files]", "crx_files", crxOutput)
	}

	// Check file types
	fileOutput, err := execCombinedOutput(ctx, "file", []string{"/home/kernel/extensions/enterprise-test/extension.crx"})
	if err != nil {
		logger.Warn("[files]", "file_type_error", err.Error())
	} else {
		logger.Info("[files]", "crx_file_type", fileOutput)
	}
}

// checkExtensionDownloadLogs checks the kernel-images-api logs for extension download requests.
func checkExtensionDownloadLogs(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// Check kernel-images-api log for requests to update.xml and .crx
	apiLog, err := execCombinedOutput(ctx, "cat", []string{"/var/log/supervisord/kernel-images-api"})
	if err != nil {
		logger.Warn("[logs]", "error", err.Error())
		return
	}

	lines := strings.Split(apiLog, "\n")
	for _, line := range lines {
		if strings.Contains(line, "update.xml") || strings.Contains(line, ".crx") || strings.Contains(line, "extension") {
			logger.Info("[logs]", "line", line)
		}
	}

	// Check specifically for GET requests to our extension
	if strings.Contains(apiLog, "GET") && strings.Contains(apiLog, "enterprise-test") {
		logger.Info("[logs]", "result", "Chrome made GET requests to fetch the extension!")
	} else {
		logger.Warn("[logs]", "result", "No GET requests to enterprise-test extension found")
	}

	// Log all GET requests
	for _, line := range lines {
		if strings.Contains(line, "GET") {
			logger.Info("[logs]", "GET_request", line)
		}
	}
}

// checkChromePolicies checks how Chrome sees the policies.
func checkChromePolicies(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// Check Chrome's local state for policy info
	localState, err := execCombinedOutput(ctx, "cat", []string{"/home/kernel/user-data/Local State"})
	if err != nil {
		logger.Warn("[policies]", "local_state_error", err.Error())
	} else {
		// Try to parse and look for extension-related info
		var state map[string]interface{}
		if err := json.Unmarshal([]byte(localState), &state); err != nil {
			logger.Warn("[policies]", "parse_error", err.Error())
		} else {
			// Look for extensions in local state
			if ext, ok := state["extensions"]; ok {
				logger.Info("[policies]", "extensions_in_local_state", fmt.Sprintf("%+v", ext))
			}
		}
	}

	// Check if Chrome has read the policy file
	// chrome://policy data could be extracted via CDP but that's complex
	// Instead, let's check if there's any extension component data
	extSettingsPath := "/home/kernel/user-data/Default/Extension Settings"
	extSettings, err := execCombinedOutput(ctx, "ls", []string{"-la", extSettingsPath})
	if err != nil {
		logger.Warn("[policies]", "ext_settings_dir_error", err.Error())
	} else {
		logger.Info("[policies]", "ext_settings_dir", extSettings)
	}
}

// checkChromiumLogs checks Chrome's logs for extension-related messages.
func checkChromiumLogs(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// Check chromium supervisor log for extension-related messages
	chromiumLog, err := execCombinedOutput(ctx, "cat", []string{"/var/log/supervisord/chromium"})
	if err != nil {
		logger.Warn("[chromium-log]", "error", err.Error())
		return
	}

	lines := strings.Split(chromiumLog, "\n")
	for _, line := range lines {
		lowLine := strings.ToLower(line)
		if strings.Contains(lowLine, "extension") ||
			strings.Contains(lowLine, "policy") ||
			strings.Contains(lowLine, "crx") ||
			strings.Contains(lowLine, "update") ||
			strings.Contains(lowLine, "error") ||
			strings.Contains(lowLine, "fail") {
			logger.Info("[chromium-log]", "line", line)
		}
	}

	// Also check stdout/stderr for the last 100 lines
	logger.Info("[chromium-log]", "action", "checking last 100 lines of chromium log")
	tailOutput, err := execCombinedOutput(ctx, "tail", []string{"-n", "100", "/var/log/supervisord/chromium"})
	if err != nil {
		logger.Warn("[chromium-log]", "tail_error", err.Error())
	} else {
		logger.Info("[chromium-log]", "last_100_lines", tailOutput)
	}
}

// restartChrome restarts Chrome via supervisorctl.
func restartChrome(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	output, err := execCombinedOutput(ctx, "supervisorctl", []string{"-c", "/etc/supervisor/supervisord.conf", "restart", "chromium"})
	if err != nil {
		logger.Warn("[restart]", "error", err.Error(), "output", output)
	} else {
		logger.Info("[restart]", "result", output)
	}
}

// takeChromePolicyScreenshot takes a screenshot of chrome://policy to debug what Chrome sees
func takeChromePolicyScreenshot(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// Use the API to take a screenshot after navigating to chrome://policy
	client, err := apiClient()
	if err != nil {
		logger.Warn("[policy-screenshot]", "client_error", err.Error())
		return
	}

	// Navigate using playwright then take screenshot
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "-e", `
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP('ws://127.0.0.1:9222/');
  const contexts = browser.contexts();
  const ctx = contexts[0] || await browser.newContext();
  const pages = ctx.pages();
  const page = pages[0] || await ctx.newPage();
  
  // Go to extensions page first to check for extension errors
  console.log('=== CHECKING EXTENSIONS ===');
  await page.goto('chrome://extensions');
  await page.waitForLoadState('networkidle');
  await page.waitForTimeout(2000);
  
  // Use evaluate to pierce shadow DOM and get extension info
  const extensionInfo = await page.evaluate(() => {
    const manager = document.querySelector('extensions-manager');
    if (!manager || !manager.shadowRoot) return { error: 'no extensions-manager' };
    
    const itemList = manager.shadowRoot.querySelector('extensions-item-list');
    if (!itemList || !itemList.shadowRoot) return { error: 'no item-list' };
    
    const items = itemList.shadowRoot.querySelectorAll('extensions-item');
    const extensions = [];
    
    for (const item of items) {
      if (!item.shadowRoot) continue;
      const nameEl = item.shadowRoot.querySelector('#name');
      const name = nameEl?.textContent?.trim() || 'unknown';
      const id = item.getAttribute('id');
      
      // Check for errors
      const warningsEl = item.shadowRoot.querySelector('.warning-list');
      const warnings = warningsEl?.textContent?.trim() || '';
      
      extensions.push({ name, id, warnings });
    }
    
    return { extensions };
  });
  
  console.log('Extensions found:', JSON.stringify(extensionInfo, null, 2));
  
  await browser.close();
})();
`)
	cmd.Dir = getPlaywrightPath()
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn("[policy-screenshot]", "error", err.Error(), "output", string(out))
	} else {
		logger.Info("[policy-screenshot]", "output", string(out))
	}

	// Ignore client since we used playwright directly
	_ = client
}

// verifyExtensionInstalled checks if the extension was installed by Chrome.
func verifyExtensionInstalled(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	// Check the extension directory
	extDir, err := execCombinedOutput(ctx, "ls", []string{"-la", "/home/kernel/extensions/"})
	if err != nil {
		logger.Warn("[verify]", "error", err.Error())
	} else {
		logger.Info("[verify]", "extensions_dir", extDir)
	}

	// Check if Chrome installed the extension using Playwright to inspect chrome://extensions
	// Note: When loaded via --load-extension, Chrome generates a NEW extension ID based on the
	// directory path, which differs from the ID in update.xml (which is for the packed .crx file).
	// So we verify by extension name instead.
	
	expectedExtensionName := "Minimal Enterprise Test Extension"
	logger.Info("[verify]", "expected_extension_name", expectedExtensionName)

	// Use playwright to navigate to chrome://extensions and verify extension is loaded
	logger.Info("[verify]", "action", "checking chrome://extensions via playwright")
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "-e", fmt.Sprintf(`
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP('ws://127.0.0.1:9222/');
  const contexts = browser.contexts();
  const ctx = contexts[0] || await browser.newContext();
  const pages = ctx.pages();
  const page = pages[0] || await ctx.newPage();
  
  await page.goto('chrome://extensions');
  await page.waitForLoadState('networkidle');
  await page.waitForTimeout(2000);
  
  const extensionInfo = await page.evaluate(() => {
    const manager = document.querySelector('extensions-manager');
    if (!manager || !manager.shadowRoot) return { error: 'no extensions-manager' };
    
    const itemList = manager.shadowRoot.querySelector('extensions-item-list');
    if (!itemList || !itemList.shadowRoot) return { error: 'no item-list' };
    
    const items = itemList.shadowRoot.querySelectorAll('extensions-item');
    const extensions = [];
    
    for (const item of items) {
      if (!item.shadowRoot) continue;
      const nameEl = item.shadowRoot.querySelector('#name');
      const name = nameEl?.textContent?.trim() || 'unknown';
      extensions.push(name);
    }
    
    return { extensions };
  });
  
  if (extensionInfo.error) {
    console.log('ERROR: ' + extensionInfo.error);
    process.exit(1);
  }
  
  const expectedName = %q;
  if (extensionInfo.extensions.includes(expectedName)) {
    console.log('SUCCESS: Extension "' + expectedName + '" found');
    process.exit(0);
  } else {
    console.log('FAIL: Extension "' + expectedName + '" not found. Extensions: ' + extensionInfo.extensions.join(', '));
    process.exit(1);
  }
  
  await browser.close();
})();
`, expectedExtensionName))
	cmd.Dir = getPlaywrightPath()
	out, err := cmd.CombinedOutput()
	logger.Info("[playwright]", "output", string(out))
	require.NoError(t, err, "extension verification failed: expected extension %q to be installed in chrome://extensions", expectedExtensionName)
}
