// Bright Data Proxy Extension - API Configured
const CONFIG_URL = "http://127.0.0.1:10001/proxy/config";

let proxyConfig = null;

// Fetch proxy configuration from API
async function fetchProxyConfig() {
  try {
    const response = await fetch(CONFIG_URL);
    if (!response.ok) {
      console.error("Failed to fetch proxy config:", response.status);
      return null;
    }
    return await response.json();
  } catch (error) {
    console.error("Error fetching proxy config:", error);
    return null;
  }
}

// Apply proxy settings
function applyProxySettings(config) {
  if (!config || !config.host || !config.port) {
    console.log("No valid proxy config, disabling proxy");
    chrome.proxy.settings.clear({ scope: "regular" });
    return;
  }

  proxyConfig = config;

  chrome.proxy.settings.set({
    value: {
      mode: "fixed_servers",
      rules: {
        singleProxy: {
          scheme: config.scheme || "http",
          host: config.host,
          port: config.port
        },
        bypassList: config.bypassList || ["localhost", "127.0.0.1"]
      }
    },
    scope: "regular"
  }, () => {
    console.log("Proxy configured:", config.host + ":" + config.port);
  });
}

// Handle proxy authentication
chrome.webRequest.onAuthRequired.addListener(
  (details, callbackFn) => {
    if (proxyConfig && proxyConfig.username && proxyConfig.password) {
      console.log("Auth required, providing credentials");
      callbackFn({
        authCredentials: {
          username: proxyConfig.username,
          password: proxyConfig.password
        }
      });
    } else {
      console.log("Auth required but no credentials available");
      callbackFn({ cancel: false });
    }
  },
  { urls: ["<all_urls>"] },
  ["asyncBlocking"]
);

// Initialize and poll for config changes
async function init() {
  const config = await fetchProxyConfig();
  if (config) {
    applyProxySettings(config);
  }
}

// Poll for config changes every 30 seconds
setInterval(async () => {
  const config = await fetchProxyConfig();
  if (config && JSON.stringify(config) !== JSON.stringify(proxyConfig)) {
    console.log("Proxy config changed, updating...");
    applyProxySettings(config);
  }
}, 30000);

init();
console.log("Bright Data Proxy Extension loaded (API-configured)");
