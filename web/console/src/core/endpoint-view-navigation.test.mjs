import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function viewSource(name) {
  return readFile(new URL(`../views/${name}.js`, import.meta.url), "utf8");
}

test("endpoint entry points navigate directly to scoped chat routes", async () => {
  const [overview, desk, login, setup] = await Promise.all([
    viewSource("OverviewView"),
    viewSource("AgentDeskView"),
    viewSource("LoginView"),
    viewSource("SetupView"),
  ]);

  assert.match(overview, /endpointRoutePath\(item\.endpoint_ref, "\/chat"\)/);
  assert.match(desk, /endpointRoutePath\(endpointRef, chatPagePath\)/);
  assert.match(login, /endpointRoutePath\(targetRef, "\/chat"\)/);
  assert.match(setup, /endpointRoutePath\(setupEndpointRef\.value, "\/chat"\)/);
});

test("endpoint-owned views keep their current endpoint in internal links", async () => {
  const [audit, chat, settings, setup] = await Promise.all([
    viewSource("AuditView"),
    viewSource("ChatView"),
    viewSource("SettingsView"),
    viewSource("SetupView"),
  ]);

  assert.match(audit, /endpointRoutePath\(endpointState\.selectedRef, "\/chat"\)/);
  assert.match(chat, /endpointRoutePath\(endpointState\.selectedRef, chatPagePath\)/);
  assert.match(settings, /endpointRoutePath\(endpointState\.selectedRef, "\/logs"\)/);
  assert.match(setup, /endpointPagePath\(route\.path\)/);
});

test("setup reads, writes, and advances only within its route endpoint", async () => {
  const [setup, repair, setupCore] = await Promise.all([
    viewSource("SetupView"),
    viewSource("RepairView"),
    readFile(new URL("./setup.js", import.meta.url), "utf8"),
  ]);

  assert.match(
    setup,
    /endpointRefFromRouteParam\(route\.params\.endpoint_ref\)/,
  );
  assert.match(
    setup,
    /endpointApiFetch\(setupEndpointRef\.value, "\/settings\/agent"/,
  );
  assert.equal(setup.match(/request: endpointApiFetch/g)?.length, 2);
  assert.match(
    setup,
    /runtimeApiFetchForEndpoint\(setupEndpointRef\.value, PERSONA_IDENTITY_ENDPOINT/,
  );
  assert.match(
    setup,
    /setupStagePath\([^\n]+, setupEndpointRef\.value\)/,
  );
  assert.doesNotMatch(
    setup,
    /runtimeApi(?:Fetch|Download)ForEndpoint\(CONSOLE_LOCAL_ENDPOINT_REF/,
  );
  assert.match(repair, /fetchConsoleSetupIntegrity\(\{[\s\S]*endpointRef: setupEndpointRef\.value/);
  assert.match(
    repair,
    /endpointApiFetch\([\s\S]*?setupEndpointRef\.value,[\s\S]*?`\/setup\/file/,
  );
  assert.match(setupCore, /const endpointRef = String\(options\.endpointRef/);
  assert.match(setupCore, /consoleStateFilesIndex\(targetEndpointRef\)/);
});
