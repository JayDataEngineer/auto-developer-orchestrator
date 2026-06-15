package browser

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/page"
)

// ──────────────────────────────────────────────
// CDP Stealth Patches
// ──────────────────────────────────────────────
//
// These evasions are based on puppeteer-extra-plugin-stealth evasions
// (https://github.com/nicedayzhu/puppeteer-extra-plugin-stealth).
// Each block comments the upstream evasion source for easy syncing.
//
// To update: check each upstream evasion file and replace the JS below
// with the latest version while keeping the Go string quoting correct.

// ── 1. navigator.webdriver ──
// Source: puppeteer-extra/packages/puppeteer-extra-plugin-stealth/evasions/navigator.webdriver/
// Overrides navigator.webdriver to be undefined/false.
const stealthWebdriverJS = `
// evade navigator.webdriver detection
try {
  delete navigator.__proto__.webdriver;
} catch(e) {}
Object.defineProperty(navigator, 'webdriver', {
  get: () => undefined,
  configurable: true,
});
`

// ── 2. chrome.runtime ──
// Source: puppeteer-extra/packages/puppeteer-extra-plugin-stealth/evasions/chrome.runtime/
// Ensures chrome.runtime exists and has realistic properties.
const stealthChromeRuntimeJS = `
// evade chrome.runtime detection
try {
  if (!window.chrome) window.chrome = {};
  if (!window.chrome.runtime) {
    window.chrome.runtime = {
      connect: () => ({ postMessage: () => {}, onMessage: { addListener: () => {} } }),
      sendMessage: () => {},
      onMessage: { addListener: () => {}, removeListener: () => {}, hasListeners: () => false },
      onConnect: { addListener: () => {}, removeListener: () => {}, hasListeners: () => false },
      onInstalled: { addListener: () => {}, removeListener: () => {}, hasListeners: () => false },
      id: 'nkeimhogjdpnpccoofpliimaahmaaome',
    };
  }
} catch(e) {}
`

// ── 3. navigator.plugins ──
// Source: puppeteer-extra/packages/puppeteer-extra-plugin-stealth/evasions/navigator.plugins/
// Overrides navigator.plugins to report real-looking entries.
// Uses a simple array with item/namedItem functions (avoids prototype issues).
const stealthPluginsJS = `
// evade navigator.plugins detection
try {
  var _plugins = [
    { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
    { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
    { name: 'Native Client', filename: 'internal-nacl-plugin', description: '' },
  ];
  function _makePluginArray(arr) {
    var p = [].concat(arr);
    p.item = function(i) { return this[i] || null; };
    p.namedItem = function(n) { for(var i=0;i<this.length;i++){if(this[i].name===n)return this[i];} return null; };
    p.refresh = function() {};
    return p;
  }
  Object.defineProperty(navigator, 'plugins', {
    get: function() { return _makePluginArray(_plugins); },
    configurable: true
  });
  Object.defineProperty(navigator, 'mimeTypes', {
    get: function() { return _makePluginArray([]); },
    configurable: true
  });
} catch(e) {}
`

// ── 4. navigator.languages ──
// Source: puppeteer-extra/packages/puppeteer-extra-plugin-stealth/evasions/navigator.languages/
// Sets realistic navigator.languages
const stealthLanguagesJS = `
// evade navigator.languages detection
try {
  Object.defineProperty(navigator, 'languages', {
    get: function() { return ['en-US', 'en']; },
    configurable: true,
  });
} catch(e) {}
`

// ── 5. navigator.permissions ──
// Source: puppeteer-extra/packages/puppeteer-extra-plugin-stealth/evasions/navigator.permissions/
// Overrides permissions.query to not reveal automation status.
const stealthPermissionsJS = `
// evade navigator.permissions detection
try {
  var originalQuery = window.navigator.permissions.query;
  window.navigator.permissions.query = function(params) {
    if (params.name === 'notifications') {
      return Promise.resolve({ state: 'prompt', onchange: null });
    }
    return originalQuery(params);
  };
  Object.defineProperty(navigator.permissions, 'query', {
    writable: false,
    enumerable: true,
    configurable: false,
    value: window.navigator.permissions.query,
  });
} catch(e) {}
`

// ── 6. WebGL vendor/renderer ──
// Source: puppeteer-extra/packages/puppeteer-extra-plugin-stealth/evasions/webgl.vendor/
// Overrides WebGL debug info to show a realistic GPU.
const stealthWebglJS = `
// evade WebGL vendor/renderer detection
try {
  var getParameter = WebGLRenderingContext.prototype.getParameter;
  WebGLRenderingContext.prototype.getParameter = function(params) {
    if (params === 37445) return 'Google Inc. (Intel)';
    if (params === 37446) return 'Intel Iris OpenGL Engine (Intel Iris OpenGL Engine)';
    return getParameter(params);
  };
  // Also patch the WebGL2 context
  if (WebGL2RenderingContext) {
    WebGL2RenderingContext.prototype.getParameter = function(params) {
      if (params === 37445) return 'Google Inc. (Intel)';
      if (params === 37446) return 'Intel Iris OpenGL Engine (Intel Iris OpenGL Engine)';
      return getParameter(params);
    };
  }
} catch(e) {}
`

// ── 7. navigator.hardwareConcurrency ──
// Sets hardware concurrency to a realistic non-2 value (2 is a known automation signal).
const stealthHardwareConcurrencyJS = `
// evade navigator.hardwareConcurrency detection
try {
  Object.defineProperty(navigator, 'hardwareConcurrency', {
    get: function() { return 8; },
    configurable: true,
  });
} catch(e) {}
`

// ── 8. navigator.deviceMemory ──
// Sets device memory to a realistic value (not undefined).
const stealthDeviceMemoryJS = `
// evade navigator.deviceMemory detection
try {
  Object.defineProperty(navigator, 'deviceMemory', {
    get: function() { return 8; },
    configurable: true,
  });
} catch(e) {}
`

// ── 9. Remove AutomationControlled flag from Chrome ──
// Some sites check chrome.loadTimes() or chrome.csi().
const stealthChromeFlagsJS = `
// fix chrome flags that signal automation
try {
  if (window.chrome && window.chrome.loadTimes) {
    var origLoadTimes = window.chrome.loadTimes;
    window.chrome.loadTimes = function() { return origLoadTimes(); };
  }
} catch(e) {}
`

// AllStealthScripts returns all stealth evasion scripts combined into one.
// They are injected via Page.addScriptToEvaluateOnNewDocument so they run
// before any page JavaScript on every document load.
func AllStealthScripts() string {
	return stealthWebdriverJS +
		stealthChromeRuntimeJS +
		stealthPluginsJS +
		stealthLanguagesJS +
		stealthPermissionsJS +
		stealthWebglJS +
		stealthHardwareConcurrencyJS +
		stealthDeviceMemoryJS +
		stealthChromeFlagsJS
}

// ApplyStealthPatches injects all stealth evasions into all frames of the
// current page using Page.addScriptToEvaluateOnNewDocument.
// This should be called once after the CDP connection and target are established.
func (sbc *SandboxBrowserClient) ApplyStealthPatches(ctx context.Context) error {
	script := AllStealthScripts()
	_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
	if err != nil {
		return fmt.Errorf("stealth injection failed: %w", err)
	}
	sbc.logger.Info("stealth patches applied via Page.addScriptToEvaluateOnNewDocument")
	return nil
}
