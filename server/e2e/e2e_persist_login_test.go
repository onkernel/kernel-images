package e2e

// These persistence tests rely on our patched Chromium (kernel-browser) which has two key modifications:
//
// 1. Session Cookie Persistence: By default, Chromium does not persist session cookies (cookies without
//    an expiration date) to disk. Our patch changes `persist_session_cookies_` to `true` in
//    `net/cookies/cookie_monster.h`, allowing session cookies like GitHub's `_gh_sess` to be saved.
//
// 2. Faster Cookie Flush: Stock Chromium only flushes cookies to SQLite every 30 seconds and after
//    512 cookie changes. Our patch reduces `kCommitInterval` to 1 second and `kCommitAfterBatchSize`
//    to 50 in `net/extras/sqlite/sqlite_persistent_cookie_store.cc`, ensuring cookies are written
//    to disk almost immediately.
//
// Without these patches, the cookie persistence tests would fail because:
// - Session cookies would never be written to the Cookies SQLite database
// - Even persistent cookies might not be flushed before we copy the user-data directory
//
// The patched Chromium is built as kernel-browser and included in the Docker images.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	logctx "github.com/onkernel/kernel-images/server/lib/logger"
	instanceoapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/stretchr/testify/require"
)

const (
	testCookieName  = "test_session"
	testCookieValue = "abc123xyz"
	testServerPort  = 18080
)

// testCookieServer is a simple HTTP server for testing cookie persistence
type testCookieServer struct {
	server *http.Server
	port   int
}

func newTestCookieServer(port int) *testCookieServer {
	mux := http.NewServeMux()

	// /set-cookie sets a cookie
	mux.HandleFunc("/set-cookie", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     testCookieName,
			Value:    testCookieValue,
			Path:     "/",
			MaxAge:   86400, // 1 day
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body><h1>Cookie Set!</h1><p>Cookie %s=%s has been set.</p></body></html>", testCookieName, testCookieValue)
	})

	// /get-cookie returns cookies as JSON
	mux.HandleFunc("/get-cookie", func(w http.ResponseWriter, r *http.Request) {
		cookies := r.Cookies()
		cookieMap := make(map[string]string)
		for _, c := range cookies {
			cookieMap[c.Name] = c.Value
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cookieMap)
	})

	// /set-indexeddb returns an HTML page with JavaScript that sets IndexedDB data
	mux.HandleFunc("/set-indexeddb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Set IndexedDB</title></head>
<body>
<h1>IndexedDB Test</h1>
<div id="status">Setting IndexedDB...</div>
<script>
const dbName = 'testDB';
const storeName = 'testStore';
const testKey = 'testKey';
const testValue = { message: 'Hello from IndexedDB!', timestamp: Date.now() };

const request = indexedDB.open(dbName, 1);

request.onupgradeneeded = function(event) {
    const db = event.target.result;
    if (!db.objectStoreNames.contains(storeName)) {
        db.createObjectStore(storeName);
    }
};

request.onsuccess = function(event) {
    const db = event.target.result;
    const transaction = db.transaction([storeName], 'readwrite');
    const store = transaction.objectStore(storeName);
    store.put(testValue, testKey);

    transaction.oncomplete = function() {
        document.getElementById('status').innerHTML = 'IndexedDB set successfully!';
        window.indexedDBSet = true;
    };

    transaction.onerror = function(e) {
        document.getElementById('status').innerHTML = 'Error: ' + e.target.error;
        window.indexedDBSet = false;
    };
};

request.onerror = function(event) {
    document.getElementById('status').innerHTML = 'Error opening IndexedDB: ' + event.target.error;
    window.indexedDBSet = false;
};
</script>
</body>
</html>`)
	})

	// /get-indexeddb returns an HTML page with JavaScript that reads IndexedDB data
	mux.HandleFunc("/get-indexeddb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Get IndexedDB</title></head>
<body>
<h1>IndexedDB Read Test</h1>
<div id="status">Reading IndexedDB...</div>
<div id="result"></div>
<script>
const dbName = 'testDB';
const storeName = 'testStore';
const testKey = 'testKey';

const request = indexedDB.open(dbName, 1);

request.onupgradeneeded = function(event) {
    // If we need to upgrade, the data doesn't exist
    document.getElementById('status').innerHTML = 'No data found (database did not exist)';
    window.indexedDBResult = null;
};

request.onsuccess = function(event) {
    const db = event.target.result;

    if (!db.objectStoreNames.contains(storeName)) {
        document.getElementById('status').innerHTML = 'No data found (store does not exist)';
        window.indexedDBResult = null;
        return;
    }

    const transaction = db.transaction([storeName], 'readonly');
    const store = transaction.objectStore(storeName);
    const getRequest = store.get(testKey);

    getRequest.onsuccess = function() {
        const value = getRequest.result;
        if (value) {
            document.getElementById('status').innerHTML = 'IndexedDB data found!';
            document.getElementById('result').innerHTML = '<pre>' + JSON.stringify(value, null, 2) + '</pre>';
            window.indexedDBResult = value;
        } else {
            document.getElementById('status').innerHTML = 'No data found for key';
            window.indexedDBResult = null;
        }
    };

    getRequest.onerror = function(e) {
        document.getElementById('status').innerHTML = 'Error reading: ' + e.target.error;
        window.indexedDBResult = null;
    };
};

request.onerror = function(event) {
    document.getElementById('status').innerHTML = 'Error opening IndexedDB: ' + event.target.error;
    window.indexedDBResult = null;
};
</script>
</body>
</html>`)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return &testCookieServer{
		server: server,
		port:   port,
	}
}

func (s *testCookieServer) Start() error {
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't fail - this runs in a goroutine
			fmt.Printf("Test server error: %v\n", err)
		}
	}()
	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	return nil
}

func (s *testCookieServer) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *testCookieServer) URL() string {
	return fmt.Sprintf("http://host.docker.internal:%d", s.port)
}


// TestCookiePersistenceHeadless tests that cookies persist across container restarts for headless image
func TestCookiePersistenceHeadless(t *testing.T) {
	testCookiePersistence(t, headlessImage, containerName+"-cookie-persist-headless")
}

// TestCookiePersistenceHeadful tests that cookies persist across container restarts for headful image
func TestCookiePersistenceHeadful(t *testing.T) {
	testCookiePersistence(t, headfulImage, containerName+"-cookie-persist-headful")
}

func testCookiePersistence(t *testing.T, image, name string) {
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Start test HTTP server
	testServer := newTestCookieServer(testServerPort)
	require.NoError(t, testServer.Start(), "failed to start test server")
	defer testServer.Stop(baseCtx)

	logger.Info("[setup]", "action", "test server started", "url", testServer.URL())

	// Clean slate
	_ = stopContainer(baseCtx, name)

	env := map[string]string{}

	// Start first container
	logger.Info("[test]", "phase", "1", "action", "starting first container")
	_, exitCh, err := runContainerWithOptions(baseCtx, image, name, env, ContainerOptions{HostAccess: true})
	require.NoError(t, err, "failed to start container: %v", err)

	ctx, cancel := context.WithTimeout(baseCtx, 5*time.Minute)
	defer cancel()

	require.NoError(t, waitHTTPOrExitWithLogs(ctx, apiBaseURL+"/spec.yaml", exitCh, name), "api not ready")

	client, err := apiClient()
	require.NoError(t, err)

	// Step 1: Verify no cookies initially
	logger.Info("[test]", "phase", "1", "action", "checking initial cookies (should be empty)")
	cookies := getCookiesViaPlaywright(t, ctx, client, testServer.URL()+"/get-cookie", logger)
	require.Empty(t, cookies, "expected no cookies initially, got: %v", cookies)

	// Step 2: Set cookie
	logger.Info("[test]", "phase", "1", "action", "setting cookie")
	setResult := navigateAndGetResult(t, ctx, client, testServer.URL()+"/set-cookie", logger)
	require.Contains(t, setResult, "Cookie Set", "expected cookie set confirmation, got: %s", setResult)

	// Step 3: Verify cookie is set
	logger.Info("[test]", "phase", "1", "action", "verifying cookie is set")
	cookies = getCookiesViaPlaywright(t, ctx, client, testServer.URL()+"/get-cookie", logger)
	require.Equal(t, testCookieValue, cookies[testCookieName], "expected cookie %s=%s, got: %v", testCookieName, testCookieValue, cookies)

	// Step 4: Wait for cookies to flush to disk (1-2 seconds with patched Chromium)
	logger.Info("[test]", "phase", "1", "action", "waiting for cookie flush to disk")
	time.Sleep(3 * time.Second)

	// Step 5: Download user-data directory
	logger.Info("[test]", "phase", "1", "action", "downloading user-data directory")
	userDataZip := downloadUserDataDir(t, ctx, client, logger)
	require.NotEmpty(t, userDataZip, "user data zip should not be empty")

	// Log what we got in the zip
	logZipContents(t, userDataZip, logger)

	// Step 6: Stop first container
	logger.Info("[test]", "phase", "1", "action", "stopping first container")
	require.NoError(t, stopContainer(ctx, name), "failed to stop container")

	// Step 7: Start second container
	logger.Info("[test]", "phase", "2", "action", "starting second container")
	_, exitCh2, err := runContainerWithOptions(ctx, image, name, env, ContainerOptions{HostAccess: true})
	require.NoError(t, err, "failed to start second container: %v", err)
	defer stopContainer(baseCtx, name)

	require.NoError(t, waitHTTPOrExitWithLogs(ctx, apiBaseURL+"/spec.yaml", exitCh2, name), "api not ready for second container")

	client2, err := apiClient()
	require.NoError(t, err)

	// Step 8: Verify no cookies in fresh container
	logger.Info("[test]", "phase", "2", "action", "verifying no cookies in fresh container")
	cookies = getCookiesViaPlaywright(t, ctx, client2, testServer.URL()+"/get-cookie", logger)
	require.Empty(t, cookies, "expected no cookies in fresh container, got: %v", cookies)

	// Step 9: Restore user-data directory
	logger.Info("[test]", "phase", "2", "action", "restoring user-data directory")
	restoreUserDataDir(t, ctx, client2, userDataZip, logger)

	// Step 10: Restart Chromium via supervisorctl
	logger.Info("[test]", "phase", "2", "action", "restarting chromium")
	restartChromium(t, ctx, client2, logger)

	// Wait for Chromium to be ready
	time.Sleep(3 * time.Second)

	// Step 11: Verify cookies are restored
	logger.Info("[test]", "phase", "2", "action", "verifying cookies are restored")
	cookies = getCookiesViaPlaywright(t, ctx, client2, testServer.URL()+"/get-cookie", logger)
	require.Equal(t, testCookieValue, cookies[testCookieName], "expected restored cookie %s=%s, got: %v", testCookieName, testCookieValue, cookies)

	logger.Info("[test]", "result", "cookie persistence test PASSED")
}

// TestIndexedDBPersistenceHeadless tests that IndexedDB data persists across container restarts for headless image
func TestIndexedDBPersistenceHeadless(t *testing.T) {
	testIndexedDBPersistence(t, headlessImage, containerName+"-idb-persist-headless")
}

// TestIndexedDBPersistenceHeadful tests that IndexedDB data persists across container restarts for headful image
func TestIndexedDBPersistenceHeadful(t *testing.T) {
	testIndexedDBPersistence(t, headfulImage, containerName+"-idb-persist-headful")
}

func testIndexedDBPersistence(t *testing.T, image, name string) {
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Start test HTTP server
	testServer := newTestCookieServer(testServerPort)
	require.NoError(t, testServer.Start(), "failed to start test server")
	defer testServer.Stop(baseCtx)

	logger.Info("[setup]", "action", "test server started", "url", testServer.URL())

	// Clean slate
	_ = stopContainer(baseCtx, name)

	env := map[string]string{}

	// Start first container
	logger.Info("[test]", "phase", "1", "action", "starting first container")
	_, exitCh, err := runContainerWithOptions(baseCtx, image, name, env, ContainerOptions{HostAccess: true})
	require.NoError(t, err, "failed to start container: %v", err)

	ctx, cancel := context.WithTimeout(baseCtx, 5*time.Minute)
	defer cancel()

	require.NoError(t, waitHTTPOrExitWithLogs(ctx, apiBaseURL+"/spec.yaml", exitCh, name), "api not ready")

	client, err := apiClient()
	require.NoError(t, err)

	// Step 1: Verify IndexedDB is empty initially
	logger.Info("[test]", "phase", "1", "action", "checking initial IndexedDB (should be empty)")
	idbResult := getIndexedDBViaPlaywright(t, ctx, client, testServer.URL()+"/get-indexeddb", logger)
	require.Nil(t, idbResult, "expected no IndexedDB data initially, got: %v", idbResult)

	// Step 2: Set IndexedDB data
	logger.Info("[test]", "phase", "1", "action", "setting IndexedDB data")
	setIndexedDBViaPlaywright(t, ctx, client, testServer.URL()+"/set-indexeddb", logger)

	// Step 3: Verify IndexedDB data is set
	logger.Info("[test]", "phase", "1", "action", "verifying IndexedDB data is set")
	idbResult = getIndexedDBViaPlaywright(t, ctx, client, testServer.URL()+"/get-indexeddb", logger)
	require.NotNil(t, idbResult, "expected IndexedDB data to be set")
	idbMap, ok := idbResult.(map[string]interface{})
	require.True(t, ok, "expected IndexedDB result to be a map, got: %T", idbResult)
	require.Equal(t, "Hello from IndexedDB!", idbMap["message"], "expected message in IndexedDB data")

	// Step 4: Wait for IndexedDB to flush to disk
	logger.Info("[test]", "phase", "1", "action", "waiting for IndexedDB flush to disk")
	time.Sleep(3 * time.Second)

	// Step 5: Download user-data directory
	logger.Info("[test]", "phase", "1", "action", "downloading user-data directory")
	userDataZip := downloadUserDataDir(t, ctx, client, logger)
	require.NotEmpty(t, userDataZip, "user data zip should not be empty")

	// Step 6: Stop first container
	logger.Info("[test]", "phase", "1", "action", "stopping first container")
	require.NoError(t, stopContainer(ctx, name), "failed to stop container")

	// Step 7: Start second container
	logger.Info("[test]", "phase", "2", "action", "starting second container")
	_, exitCh2, err := runContainerWithOptions(ctx, image, name, env, ContainerOptions{HostAccess: true})
	require.NoError(t, err, "failed to start second container: %v", err)
	defer stopContainer(baseCtx, name)

	require.NoError(t, waitHTTPOrExitWithLogs(ctx, apiBaseURL+"/spec.yaml", exitCh2, name), "api not ready for second container")

	client2, err := apiClient()
	require.NoError(t, err)

	// Step 8: Verify IndexedDB is empty in fresh container
	logger.Info("[test]", "phase", "2", "action", "verifying IndexedDB is empty in fresh container")
	idbResult = getIndexedDBViaPlaywright(t, ctx, client2, testServer.URL()+"/get-indexeddb", logger)
	require.Nil(t, idbResult, "expected no IndexedDB data in fresh container, got: %v", idbResult)

	// Step 9: Restore user-data directory
	logger.Info("[test]", "phase", "2", "action", "restoring user-data directory")
	restoreUserDataDir(t, ctx, client2, userDataZip, logger)

	// Step 10: Restart Chromium via supervisorctl
	logger.Info("[test]", "phase", "2", "action", "restarting chromium")
	restartChromium(t, ctx, client2, logger)

	// Wait for Chromium to be ready
	time.Sleep(3 * time.Second)

	// Step 11: Verify IndexedDB data is restored
	logger.Info("[test]", "phase", "2", "action", "verifying IndexedDB data is restored")
	idbResult = getIndexedDBViaPlaywright(t, ctx, client2, testServer.URL()+"/get-indexeddb", logger)
	require.NotNil(t, idbResult, "expected IndexedDB data to be restored")
	idbMap, ok = idbResult.(map[string]interface{})
	require.True(t, ok, "expected IndexedDB result to be a map, got: %T", idbResult)
	require.Equal(t, "Hello from IndexedDB!", idbMap["message"], "expected message in restored IndexedDB data")

	logger.Info("[test]", "result", "IndexedDB persistence test PASSED")
}

// getCookiesViaPlaywright navigates to a URL and returns the cookies as a map
func getCookiesViaPlaywright(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, url string, logger *slog.Logger) map[string]string {
	code := fmt.Sprintf(`
		await page.goto('%s');
		const content = await page.textContent('body');
		return content;
	`, url)

	req := instanceoapi.ExecutePlaywrightCodeJSONRequestBody{Code: code}
	rsp, err := client.ExecutePlaywrightCodeWithResponse(ctx, req)
	require.NoError(t, err, "playwright execute request error")
	require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))
	require.NotNil(t, rsp.JSON200, "expected JSON200 response")

	if !rsp.JSON200.Success {
		var errorMsg string
		if rsp.JSON200.Error != nil {
			errorMsg = *rsp.JSON200.Error
		}
		t.Fatalf("playwright execution failed: %s", errorMsg)
	}

	// Parse the JSON result
	resultStr, ok := rsp.JSON200.Result.(string)
	if !ok {
		// Try to marshal and unmarshal
		resultBytes, _ := json.Marshal(rsp.JSON200.Result)
		resultStr = string(resultBytes)
		// Remove quotes if present
		resultStr = strings.Trim(resultStr, "\"")
	}

	logger.Info("[playwright]", "raw_result", resultStr)

	var cookies map[string]string
	if err := json.Unmarshal([]byte(resultStr), &cookies); err != nil {
		// If it's not valid JSON, return empty
		logger.Info("[playwright]", "parse_error", err.Error())
		return make(map[string]string)
	}

	return cookies
}

// navigateAndGetResult navigates to a URL and returns the page content
func navigateAndGetResult(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, url string, logger *slog.Logger) string {
	code := fmt.Sprintf(`
		await page.goto('%s');
		const content = await page.textContent('body');
		return content;
	`, url)

	req := instanceoapi.ExecutePlaywrightCodeJSONRequestBody{Code: code}
	rsp, err := client.ExecutePlaywrightCodeWithResponse(ctx, req)
	require.NoError(t, err, "playwright execute request error")
	require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))
	require.NotNil(t, rsp.JSON200, "expected JSON200 response")
	require.True(t, rsp.JSON200.Success, "expected success=true")

	resultStr, ok := rsp.JSON200.Result.(string)
	if !ok {
		resultBytes, _ := json.Marshal(rsp.JSON200.Result)
		resultStr = string(resultBytes)
	}

	return resultStr
}

// getIndexedDBViaPlaywright navigates to a page and reads IndexedDB data
func getIndexedDBViaPlaywright(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, url string, logger *slog.Logger) interface{} {
	// Navigate to the page and read IndexedDB directly via page.evaluate
	code := fmt.Sprintf(`
		await page.goto('%s', { waitUntil: 'domcontentloaded' });

		// Directly read IndexedDB in the page context
		const result = await page.evaluate(async () => {
			return new Promise((resolve) => {
				const dbName = 'testPersistDB';
				const storeName = 'testStore';
				const testKey = 'testKey';

				const request = indexedDB.open(dbName, 1);

				request.onupgradeneeded = function(event) {
					// If we need to upgrade, the data doesn't exist
					event.target.transaction.abort();
					resolve(null);
				};

				request.onsuccess = function(event) {
					const db = event.target.result;

					if (!db.objectStoreNames.contains(storeName)) {
						db.close();
						resolve(null);
						return;
					}

					const transaction = db.transaction([storeName], 'readonly');
					const store = transaction.objectStore(storeName);
					const getRequest = store.get(testKey);

					getRequest.onsuccess = function() {
						db.close();
						resolve(getRequest.result || null);
					};

					getRequest.onerror = function() {
						db.close();
						resolve(null);
					};
				};

				request.onerror = function() {
					resolve(null);
				};

				// Timeout after 5 seconds
				setTimeout(() => {
					resolve(null);
				}, 5000);
			});
		});

		return result;
	`, url)

	req := instanceoapi.ExecutePlaywrightCodeJSONRequestBody{Code: code}
	rsp, err := client.ExecutePlaywrightCodeWithResponse(ctx, req)
	require.NoError(t, err, "playwright execute request error")
	require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))
	require.NotNil(t, rsp.JSON200, "expected JSON200 response")

	if !rsp.JSON200.Success {
		var errMsg string
		if rsp.JSON200.Error != nil {
			errMsg = *rsp.JSON200.Error
		}
		logger.Info("[getIndexedDB]", "error", errMsg)
	}

	require.True(t, rsp.JSON200.Success, "expected success=true")

	logger.Info("[getIndexedDB]", "result", rsp.JSON200.Result)
	return rsp.JSON200.Result
}

// setIndexedDBViaPlaywright navigates to the IndexedDB set page and waits for completion
func setIndexedDBViaPlaywright(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, url string, logger *slog.Logger) {
	// Navigate to the page and set IndexedDB directly via page.evaluate
	// Use a unique timestamp-based version to ensure onupgradeneeded is called
	code := fmt.Sprintf(`
		await page.goto('%s', { waitUntil: 'domcontentloaded' });

		// Check if IndexedDB is available
		const idbAvailable = await page.evaluate(() => !!window.indexedDB);
		if (!idbAvailable) {
			return { success: false, error: 'IndexedDB not available' };
		}

		// Directly execute IndexedDB operations in the page context
		const result = await page.evaluate(() => {
			return new Promise((resolve) => {
				const dbName = 'testPersistDB';
				const storeName = 'testStore';
				const testKey = 'testKey';
				const testValue = { message: 'Hello from IndexedDB!', timestamp: Date.now() };

				// First, delete any existing database to ensure clean state
				const deleteRequest = indexedDB.deleteDatabase(dbName);

				deleteRequest.onsuccess = function() {
					// Now create fresh database with object store
					const openRequest = indexedDB.open(dbName, 1);

					openRequest.onupgradeneeded = function(event) {
						try {
							const db = event.target.result;
							db.createObjectStore(storeName);
						} catch (e) {
							resolve({ success: false, error: 'onupgradeneeded error: ' + e.toString() });
						}
					};

					openRequest.onsuccess = function(event) {
						try {
							const db = event.target.result;
							const transaction = db.transaction([storeName], 'readwrite');
							const store = transaction.objectStore(storeName);
							const putRequest = store.put(testValue, testKey);

							putRequest.onsuccess = function() {
								db.close();
								resolve({ success: true, message: 'IndexedDB put succeeded' });
							};

							putRequest.onerror = function(e) {
								db.close();
								resolve({ success: false, error: 'Put error: ' + (e.target?.error?.toString() || 'unknown') });
							};
						} catch (e) {
							resolve({ success: false, error: 'onsuccess error: ' + e.toString() });
						}
					};

					openRequest.onerror = function(event) {
						resolve({ success: false, error: 'Open error: ' + (event.target?.error?.toString() || 'unknown') });
					};
				};

				deleteRequest.onerror = function() {
					resolve({ success: false, error: 'Delete error' });
				};

				// Timeout after 5 seconds
				setTimeout(() => {
					resolve({ success: false, error: 'Timeout waiting for IndexedDB' });
				}, 5000);
			});
		});

		return result;
	`, url)

	req := instanceoapi.ExecutePlaywrightCodeJSONRequestBody{Code: code}
	rsp, err := client.ExecutePlaywrightCodeWithResponse(ctx, req)
	require.NoError(t, err, "playwright execute request error")
	require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))
	require.NotNil(t, rsp.JSON200, "expected JSON200 response")

	if !rsp.JSON200.Success {
		var errMsg string
		if rsp.JSON200.Error != nil {
			errMsg = *rsp.JSON200.Error
		}
		var stderr string
		if rsp.JSON200.Stderr != nil {
			stderr = *rsp.JSON200.Stderr
		}
		logger.Info("[setIndexedDB]", "error", errMsg, "stderr", stderr)
		require.True(t, rsp.JSON200.Success, "expected success=true, error: %s", errMsg)
	}

	logger.Info("[setIndexedDB]", "result", rsp.JSON200.Result)

	// The result should be an object with success and message
	if resultMap, ok := rsp.JSON200.Result.(map[string]interface{}); ok {
		if success, ok := resultMap["success"].(bool); ok {
			require.True(t, success, "expected IndexedDB set to succeed, got error: %v", resultMap["error"])
		}
	}
}

// downloadUserDataDir downloads the user-data directory as a zip
func downloadUserDataDir(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, logger *slog.Logger) []byte {
	params := &instanceoapi.DownloadDirZipParams{
		Path: "/home/kernel/user-data",
	}

	rsp, err := client.DownloadDirZipWithResponse(ctx, params)
	require.NoError(t, err, "download dir zip request error")
	require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status: %s", rsp.Status())

	logger.Info("[download]", "size_bytes", len(rsp.Body))
	return rsp.Body
}

// logZipContents logs the contents of a zip file for debugging
func logZipContents(t *testing.T, zipData []byte, logger *slog.Logger) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		logger.Info("[zip]", "error", "failed to read zip", "err", err.Error())
		return
	}

	var files []string
	for _, f := range reader.File {
		files = append(files, f.Name)
	}

	logger.Info("[zip]", "contents", strings.Join(files, ", "))
}

// restoreUserDataDir uploads and extracts user-data directory from a zip
func restoreUserDataDir(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, zipData []byte, logger *slog.Logger) {
	// First, we need to extract the zip and upload files individually
	// The API has WriteFile but not a direct "upload zip and extract" endpoint
	// We'll use ProcessExec to extract after uploading

	// Upload the zip file to a temp location
	zipPath := "/tmp/user-data-restore.zip"
	params := &instanceoapi.WriteFileParams{
		Path: zipPath,
	}

	rsp, err := client.WriteFileWithBodyWithResponse(ctx, params, "application/octet-stream", bytes.NewReader(zipData))
	require.NoError(t, err, "write file request error")
	require.Equal(t, http.StatusCreated, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))

	logger.Info("[restore]", "action", "uploaded zip", "path", zipPath)

	// Extract the zip using unzip command
	args := []string{"-o", zipPath, "-d", "/home/kernel/user-data"}
	req := instanceoapi.ProcessExecJSONRequestBody{
		Command: "unzip",
		Args:    &args,
	}

	execRsp, err := client.ProcessExecWithResponse(ctx, req)
	require.NoError(t, err, "process exec request error")
	require.Equal(t, http.StatusOK, execRsp.StatusCode(), "unexpected status: %s body=%s", execRsp.Status(), string(execRsp.Body))

	if execRsp.JSON200.ExitCode != nil && *execRsp.JSON200.ExitCode != 0 {
		var stdout, stderr string
		if execRsp.JSON200.StdoutB64 != nil {
			if b, decErr := base64.StdEncoding.DecodeString(*execRsp.JSON200.StdoutB64); decErr == nil {
				stdout = string(b)
			}
		}
		if execRsp.JSON200.StderrB64 != nil {
			if b, decErr := base64.StdEncoding.DecodeString(*execRsp.JSON200.StderrB64); decErr == nil {
				stderr = string(b)
			}
		}
		require.Fail(t, "unzip failed", "exit_code=%d stdout=%s stderr=%s", *execRsp.JSON200.ExitCode, stdout, stderr)
	}

	logger.Info("[restore]", "action", "extracted zip to user-data")

	// Remove lock files that prevent Chromium from starting with restored profile
	lockFiles := []string{
		"/home/kernel/user-data/SingletonLock",
		"/home/kernel/user-data/SingletonSocket",
		"/home/kernel/user-data/SingletonCookie",
	}
	for _, lockFile := range lockFiles {
		rmArgs := []string{"-f", lockFile}
		rmReq := instanceoapi.ProcessExecJSONRequestBody{
			Command: "rm",
			Args:    &rmArgs,
		}
		_, _ = client.ProcessExecWithResponse(ctx, rmReq)
	}
	logger.Info("[restore]", "action", "removed lock files")

	// Fix permissions
	chownArgs := []string{"-R", "kernel:kernel", "/home/kernel/user-data"}
	chownReq := instanceoapi.ProcessExecJSONRequestBody{
		Command: "chown",
		Args:    &chownArgs,
	}
	_, _ = client.ProcessExecWithResponse(ctx, chownReq)

	logger.Info("[restore]", "action", "fixed permissions")
}

// restartChromium restarts Chromium via supervisorctl and waits for it to be ready
func restartChromium(t *testing.T, ctx context.Context, client *instanceoapi.ClientWithResponses, logger *slog.Logger) {
	args := []string{"-c", "/etc/supervisor/supervisord.conf", "restart", "chromium"}
	req := instanceoapi.ProcessExecJSONRequestBody{
		Command: "supervisorctl",
		Args:    &args,
	}

	rsp, err := client.ProcessExecWithResponse(ctx, req)
	require.NoError(t, err, "supervisorctl restart request error")
	require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))

	logger.Info("[restart]", "action", "chromium restarted via supervisorctl")

	// Wait for CDP endpoint to be ready again by checking the internal CDP endpoint
	logger.Info("[restart]", "action", "waiting for CDP endpoint to be ready")
	for i := 0; i < 30; i++ {
		// Use curl to check the CDP endpoint and capture the HTTP status code
		checkArgs := []string{"-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:9223/json/version"}
		checkReq := instanceoapi.ProcessExecJSONRequestBody{
			Command: "curl",
			Args:    &checkArgs,
		}
		checkRsp, err := client.ProcessExecWithResponse(ctx, checkReq)
		if err == nil && checkRsp.JSON200 != nil && checkRsp.JSON200.ExitCode != nil && *checkRsp.JSON200.ExitCode == 0 {
			// Decode stdout to get the HTTP status code
			if checkRsp.JSON200.StdoutB64 != nil {
				if b, decErr := base64.StdEncoding.DecodeString(*checkRsp.JSON200.StdoutB64); decErr == nil {
					httpCode := strings.TrimSpace(string(b))
					if httpCode == "200" {
						logger.Info("[restart]", "action", "CDP endpoint is ready", "http_code", httpCode)
						return
					}
					logger.Info("[restart]", "action", "CDP endpoint returned non-200", "http_code", httpCode)
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	require.Fail(t, "Chromium restart timed out", "CDP endpoint did not become ready after 15 seconds")
}
