import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const setupContractSource = new URL("./setup-contract.js", import.meta.url);
const inferenceProviderPickerSource = new URL("../components/InferenceProviderPicker.js", import.meta.url);
const statsViewSource = new URL("../views/StatsView.js", import.meta.url);

async function readSetupContract() {
  return readFile(setupContractSource, "utf8");
}

test("setup contract exposes Sakana AI as an inference provider", async () => {
  const source = await readSetupContract();

  assert.match(source, /const SETUP_PROVIDER_SAKANA = "sakana"/);
  assert.match(source, /\{ title: "Sakana AI", value: SETUP_PROVIDER_SAKANA \}/);
  assert.match(source, /\[SETUP_PROVIDER_SAKANA\]: \{ supportsModelLookup: true \}/);
  assert.match(source, /case SETUP_PROVIDER_SAKANA:\s+return SETUP_PROVIDER_SAKANA;/);
  assert.match(source, /\[SETUP_PROVIDER_SAKANA\]: \{\s+title: "Sakana AI",\s+url: "https:\/\/console\.sakana\.ai\/"/);
  assert.match(source, /SETUP_PROVIDER_SAKANA,/);
});

test("Sakana AI uses its logo in provider surfaces", async () => {
  const pickerSource = await readFile(inferenceProviderPickerSource, "utf8");
  const statsSource = await readFile(statsViewSource, "utf8");

  assert.match(pickerSource, /import sakanaLogo from "\.\.\/assets\/model-vendors\/sakana\.svg"/);
  assert.match(pickerSource, /sakana: \{ src: sakanaLogo, className: "is-sakana" \}/);
  assert.match(statsSource, /import sakanaIcon from "\.\.\/assets\/model-vendors\/sakana\.svg"/);
  assert.match(statsSource, /sakana: sakanaIcon/);
});
