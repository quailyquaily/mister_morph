function currentWindow() {
  return typeof window === "undefined" ? null : window;
}

function currentNavigator() {
  return typeof navigator === "undefined" ? null : navigator;
}

function currentDocument() {
  return typeof document === "undefined" ? null : document;
}

function majorVersion(value) {
  const match = String(value || "").trim().match(/^(\d+)/);
  return match ? Number.parseInt(match[1], 10) : 0;
}

function macOSVersionFromUserAgent(userAgent) {
  const match = String(userAgent || "").match(/Mac OS X\s+(\d+(?:[._]\d+)*)/i);
  return match ? majorVersion(match[1]) : 0;
}

function browserReportsMacOS(navigatorObject) {
  const clientPlatform = String(navigatorObject?.userAgentData?.platform || "").trim();
  if (clientPlatform) {
    return clientPlatform.toLowerCase() === "macos";
  }
  const platform = String(navigatorObject?.platform || "");
  const userAgent = String(navigatorObject?.userAgent || "");
  return /^Mac/i.test(platform) || /Macintosh|Mac OS X/i.test(userAgent);
}

export async function detectMacOSMajorVersion({
  windowObject = currentWindow(),
  navigatorObject = currentNavigator(),
} = {}) {
  const desktopPlatform = windowObject?.__MISTERMORPH_DESKTOP_PLATFORM__;
  if (windowObject?.__MISTERMORPH_DESKTOP_RUNTIME__ === true && desktopPlatform) {
    return String(desktopPlatform.os || "").toLowerCase() === "darwin"
      ? majorVersion(desktopPlatform.version)
      : 0;
  }

  if (!browserReportsMacOS(navigatorObject)) {
    return 0;
  }

  const userAgentData = navigatorObject?.userAgentData;
  if (typeof userAgentData?.getHighEntropyValues === "function") {
    try {
      const values = await userAgentData.getHighEntropyValues(["platformVersion"]);
      const detected = majorVersion(values?.platformVersion);
      if (detected > 0) {
        return detected;
      }
    } catch {
      // Fall back to the legacy user agent when client hints are unavailable.
    }
  }

  return macOSVersionFromUserAgent(navigatorObject?.userAgent);
}

export async function installMacOS26Mode({
  documentObject = currentDocument(),
  windowObject = currentWindow(),
  navigatorObject = currentNavigator(),
} = {}) {
  const root = documentObject?.documentElement;
  if (!root) {
    return false;
  }
  const enabled = (await detectMacOSMajorVersion({ windowObject, navigatorObject })) >= 26;
  if (enabled) {
    root.dataset.macos26 = "true";
  } else {
    delete root.dataset.macos26;
  }
  return enabled;
}
