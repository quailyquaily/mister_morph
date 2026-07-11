import assert from "node:assert/strict";
import test from "node:test";

import {
  detectMacOSMajorVersion,
  installMacOS26Mode,
} from "./platform.js";

test("detects macOS 26 from the desktop host platform", async () => {
  const major = await detectMacOSMajorVersion({
    windowObject: {
      __MISTERMORPH_DESKTOP_RUNTIME__: true,
      __MISTERMORPH_DESKTOP_PLATFORM__: {
        os: "darwin",
        version: "26.1.0",
      },
    },
    navigatorObject: {},
  });

  assert.equal(major, 26);
});

test("prefers desktop host platform over browser user agent", async () => {
  const major = await detectMacOSMajorVersion({
    windowObject: {
      __MISTERMORPH_DESKTOP_RUNTIME__: true,
      __MISTERMORPH_DESKTOP_PLATFORM__: {
        os: "darwin",
        version: "25.6.0",
      },
    },
    navigatorObject: {
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 26_0)",
    },
  });

  assert.equal(major, 25);
});

test("detects macOS 26 from browser user agent client hints", async () => {
  const major = await detectMacOSMajorVersion({
    windowObject: {},
    navigatorObject: {
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
      userAgentData: {
        platform: "macOS",
        async getHighEntropyValues(hints) {
          assert.deepEqual(hints, ["platformVersion"]);
          return { platformVersion: "26.0.0" };
        },
      },
    },
  });

  assert.equal(major, 26);
});

test("detects macOS 26 from an unfrozen browser user agent", async () => {
  const major = await detectMacOSMajorVersion({
    windowObject: {},
    navigatorObject: {
      platform: "MacIntel",
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 26_2_1)",
    },
  });

  assert.equal(major, 26);
});

test("does not treat Safari's frozen macOS user agent as macOS 26", async () => {
  const major = await detectMacOSMajorVersion({
    windowObject: {},
    navigatorObject: {
      platform: "MacIntel",
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/26.0 Safari/605.1.15",
    },
  });

  assert.equal(major, 10);
});

test("ignores non-macOS browsers", async () => {
  const major = await detectMacOSMajorVersion({
    windowObject: {},
    navigatorObject: {
      platform: "Win32",
      userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
      userAgentData: {
        platform: "Windows",
        async getHighEntropyValues() {
          return { platformVersion: "26.0.0" };
        },
      },
    },
  });

  assert.equal(major, 0);
});

test("installs the macOS 26 mode only for supported versions", async () => {
  const root = { dataset: {} };
  const enabled = await installMacOS26Mode({
    documentObject: { documentElement: root },
    windowObject: {
      __MISTERMORPH_DESKTOP_RUNTIME__: true,
      __MISTERMORPH_DESKTOP_PLATFORM__: {
        os: "darwin",
        version: "27.0",
      },
    },
    navigatorObject: {},
  });

  assert.equal(enabled, true);
  assert.equal(root.dataset.macos26, "true");

  const unsupportedRoot = { dataset: {} };
  const unsupported = await installMacOS26Mode({
    documentObject: { documentElement: unsupportedRoot },
    windowObject: {},
    navigatorObject: {
      platform: "MacIntel",
      userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7)",
    },
  });

  assert.equal(unsupported, false);
  assert.equal(unsupportedRoot.dataset.macos26, undefined);
});
