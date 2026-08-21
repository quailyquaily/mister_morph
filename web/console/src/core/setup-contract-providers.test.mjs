import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const setupContractSource = new URL("./setup-contract.js", import.meta.url);

async function readSetupContract() {
  return readFile(setupContractSource, "utf8");
}

test("setup contract exposes Sakana AI as an inference provider", async () => {
  const source = await readSetupContract();

  assert.match(source, /const SETUP_PROVIDER_SAKANA = "sakana"/);
  assert.match(source, /\{ title: "Sakana AI", value: SETUP_PROVIDER_SAKANA, group: "api" \}/);
  assert.match(source, /\[SETUP_PROVIDER_SAKANA\]: \{ supportsModelLookup: true \}/);
  assert.match(source, /case SETUP_PROVIDER_SAKANA:\s+return SETUP_PROVIDER_SAKANA;/);
  assert.match(source, /\[SETUP_PROVIDER_SAKANA\]: \{\s+title: "Sakana AI",\s+url: "https:\/\/console\.sakana\.ai\/"/);
  assert.match(source, /SETUP_PROVIDER_SAKANA,/);
});

test("setup contract exposes Meta Model API as an inference provider", async () => {
  const source = await readSetupContract();

  assert.match(source, /const SETUP_PROVIDER_META = "meta"/);
  assert.match(source, /\{ title: "Meta", value: SETUP_PROVIDER_META, group: "api" \}/);
  assert.match(source, /\[SETUP_PROVIDER_META\]: \{\}/);
  assert.match(source, /case SETUP_PROVIDER_META:\s+return SETUP_PROVIDER_META;/);
  assert.match(source, /\[SETUP_PROVIDER_META\]: \{\s+title: "Meta Model API",\s+url: "https:\/\/developer\.meta\.com\/ai\/"/);
  assert.match(source, /SETUP_PROVIDER_META,/);
});

test("OpenAI Codex uses API key auth when endpoint and API key are both present", async () => {
	const source = await readSetupContract();

	assert.match(source, /\[SETUP_PROVIDER_OPENAI_CODEX\]: \{ supportsCustomAPIBase: true, supportsAPIKey: true \}/);
	assert.match(source, /function setupProviderSupportsCustomAPIBase\(choice\)/);
	assert.match(source, /function setupProviderSupportsAPIKey\(choice\)/);
	assert.match(source, /function setupOpenAICodexUsesAPIKey\(endpoint, hasAPIKey\)/);
	assert.match(source, /return String\(endpoint \|\| ""\)\.trim\(\) !== "" && hasAPIKey === true;/);
	assert.match(source, /setupProviderSupportsCustomAPIBase,/);
	assert.match(source, /setupProviderSupportsAPIKey,/);
	assert.match(source, /setupOpenAICodexUsesAPIKey,/);
	assert.doesNotMatch(source, /\[SETUP_PROVIDER_OPENAI_CODEX\]: \{[^}]*requiresAPIBase: true/);
});
