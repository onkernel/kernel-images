const {chromium} = require('playwright');
var http = require('http');
var httpProxy = require('http-proxy');
var modifyResponse = require('node-http-proxy-json');
var devtoolsPath = ""
var sem = require('semaphore')(1);

const port = 8080; // Use port 8080 by default
const cdp_host = '127.0.0.1';
const cdp_port = 9222;

const Debug = require('debug');

// Create a debug instance for Playwright's logging
const debug = Debug('pw:*');

// Override the default log function to include timestamps
debug.log = function (...args) {
    const timestamp = new Date().toISOString();
    const formattedArgs = args.map(arg => (typeof arg === 'string' ? arg : JSON.stringify(arg)));
    console.log(`[${timestamp}] ${formattedArgs.join(' ')}`);
};

var log = console.log;

console.log = function () {
    var first_parameter = arguments[0];
    var other_parameters = Array.prototype.slice.call(arguments, 1);

    function formatConsoleDate (date) {
        var hour = date.getHours();
        var minutes = date.getMinutes();
        var seconds = date.getSeconds();
        var milliseconds = date.getMilliseconds();

        return '[' +
               ((hour < 10) ? '0' + hour: hour) +
               ':' +
               ((minutes < 10) ? '0' + minutes: minutes) +
               ':' +
               ((seconds < 10) ? '0' + seconds: seconds) +
               '.' +
               ('00' + milliseconds).slice(-3) +
               '] ';
    }

    log.apply(console, [formatConsoleDate(new Date()) + first_parameter].concat(other_parameters));
};

//
// Launch Chromium browser with CDP enabled.
//
function startBrowser() {
    return new Promise((resolve, reject) => {
        chromium.launch({headless: true, args: [`--remote-debugging-port=${cdp_port}`, "--v=2"],
            logger: {
                isEnabled: () => true,
                log: (name, severity, message, args) => console.log(`${name} ${message}`)
            }});
        resolve();
    });
}

//
// Check if CDP is ready for connections.
//
function checkCdpReady() {
    return new Promise((resolve, reject) => {
        const options = {
            hostname: cdp_host,
            port: cdp_port,
            path: '/json/version',
            method: 'GET',
            json: true
        };

        const req = http.request(options, (res) => {
            if (res.statusCode === 200) {
                resolve(true); // Server is ready
            } else {
                reject(new Error(`Unexpected status code: ${res.statusCode}`));
            }
        });

        req.on('error', (error) => {
            if (error.code === 'ECONNREFUSED') {
                reject(error); // Server is not ready yet
            } else {
                reject(new Error(`Request error: ${error.message}`));
            }
        });

        req.end();
    });
}

async function waitForDevtools(host, port) {
    const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

    while (true) {
        try {
            await checkCdpReady(host, port);
            console.log('CDP is ready!');
            break; // Exit the loop when the server is ready
        } catch (error) {
            console.log('Server not ready, retrying in 1 second...');
            await delay(1000); // Wait 1 second before retrying
        }
    }
}

//
// Get CDP URL.
//
function getDevtoolsPath() {
    return new Promise((resolve, reject) => {
        var options = {
            hostname: cdp_host,
            port: cdp_port,
            path: '/json/version',
            method: 'GET',
            json: true,
            timeout: 5000
        };

        var req = http.get(options, function(res) {
            let output = '';
            res.setEncoding('utf8');

            res.on('data', function(chunk) {
                output += chunk;
            });

            res.on('end', () => {
                try {
                    let obj = JSON.parse(output);
                    console.log("debuggerUrl: " + obj.webSocketDebuggerUrl);
                    devtoolsPath = obj.webSocketDebuggerUrl.replace(`ws://${cdp_host}:${cdp_port}`, "");
                    console.log("devtoolsPath: " + devtoolsPath);
                    resolve();
                }
                catch (err) {
                    console.error('rest::end', err);
                    reject(err);
                }
            });
        });

        req.on('error', (error) => {
            reject(`Problem with request: ${error.message}`);
        });

        req.end();
    });
}

function runProxy() {
    //
    // Set up our server to proxy standard HTTP requests.
    //
    var proxy = new httpProxy.createProxyServer({
        target: {
            host: cdp_host,
            port: cdp_port
        }
    });
    var proxyServer = http.createServer(function (req, res) {
        console.log("server request URL: " + req.url);
        req.headers['host'] = `${cdp_host}:${cdp_port}`;
        proxy.web(req, res);
    });

    proxy.on('proxyReq', function(proxyReq, req, res) {
        console.log("request URL: " + req.url);
    });

    //
    // Listen for the `proxyRes` event on `proxy`.
    // Update WebSocket URL in JSON response from Chrome CDP.
    //
    proxy.on('proxyRes', function (proxyRes, req, res) {
        if (res.req.url.startsWith("/json")) {
            console.log("URL: " + res.req.url);
            const isHost = (element) => element == 'Host';
            host = res.req.rawHeaders[res.req.rawHeaders.findIndex(isHost)+1];

            modifyResponse(res, proxyRes, function (body) {
                if (body) {
                    console.log("debugger URL: " + body.webSocketDebuggerUrl);
                    console.log(`new URL: wss://${host}/unikraft`);
                    devtoolsPath = body.webSocketDebuggerUrl.replace(`ws://${cdp_host}:${cdp_port}`, "");
                    console.log(`devtoolsPath: ${devtoolsPath}`);
                    //body.webSocketDebuggerUrl = body.webSocketDebuggerUrl.replace(`${cdp_host}:${cdp_port}`, host);
                    body.webSocketDebuggerUrl = `wss://${host}/unikraft`;
                    //body.webSocketDebuggerUrl = body.webSocketDebuggerUrl.replace("ws://", "wss://");
                }
                return body; // return value can be a promise
            });

            res.setHeader('Host', host);
        }
    });

    //
    // Listen to the `upgrade` event and proxy the
    // WebSocket requests as well.
    //
    proxyServer.on('upgrade', function (req, socket, head) {
        console.log("before take (upgrade)");
        console.log("upgrade: devtoolsPath: " + devtoolsPath);
        sem.take(function() {
            console.log("upgrade request URL: " + req.url);
            req.url = devtoolsPath;
            proxy.ws(req, socket, head);
        });
    });

    //
    // Listen for the `close` event on `proxy`.
    //
    proxy.on('close', function (res, socket, head) {
        console.log("websocket closed");
        sem.leave();
        console.log("after leave");
    });

    proxyServer.listen(port, () => {
        console.log('Server is running on port ' + port);
    });
}

async function main() {
    try {
        await startBrowser();
        await waitForDevtools();
        await getDevtoolsPath();
        runProxy();
    } catch (error) {
        console.error(error);
    }
}

// Start the main function
main();
