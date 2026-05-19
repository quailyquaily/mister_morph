import assert from "node:assert/strict";
import test from "node:test";

import { buildIdentityYAML, parseIdentityProfile } from "./persona-profile.js";

test("buildIdentityYAML preserves name_alts and unknown fields", () => {
  const previous = [
    "name: Old",
    "name_alts:",
    "  - Lyric",
    "creature: Human",
    "vibe: Old vibe",
    "emoji: old",
    "custom:",
    "  nested: true",
    "",
  ].join("\n");

  const next = buildIdentityYAML(
    {
      name: "New",
      creature: "Engineer",
      vibe: "Plain\nDirect",
      emoji: ":)",
    },
    previous
  );

  assert.match(next, /name_alts:\n\s+- Lyric/);
  assert.match(next, /custom:\n\s+nested: true/);
  assert.equal(parseIdentityProfile(next).name, "New");
  assert.equal(parseIdentityProfile(next).creature, "Engineer");
  assert.equal(parseIdentityProfile(next).vibe, "Plain\nDirect");
});

test("parseIdentityProfile reads YAML block scalars", () => {
  const profile = parseIdentityProfile([
    "name: Ada",
    "creature: Human",
    "vibe: |",
    "  Calm",
    "  Precise",
    "emoji: test",
    "",
  ].join("\n"));

  assert.equal(profile.name, "Ada");
  assert.equal(profile.vibe, "Calm\nPrecise");
});
