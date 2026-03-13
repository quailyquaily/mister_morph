function getRestartBinding() {
  if (!window || typeof window !== "object") {
    return null;
  }
  const fn = window.go && window.go.main && window.go.main.App && window.go.main.App.RestartApp;
  return typeof fn === "function" ? fn : null;
}

async function requestDesktopRestart() {
  const restart = getRestartBinding();
  if (!restart) {
    return false;
  }
  try {
    await restart();
    return true;
  } catch {
    return false;
  }
}

function isDesktopHostLikely() {
  return getRestartBinding() !== null;
}

export { requestDesktopRestart, isDesktopHostLikely };
