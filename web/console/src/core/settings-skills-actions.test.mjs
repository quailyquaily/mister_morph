import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const settingsViewSource = new URL("../views/SettingsView.js", import.meta.url);

test("skills settings use card actions instead of direct load-list editing", async () => {
  const source = await readFile(settingsViewSource, "utf8");

  assert.match(source, /function setSkillLoaded\(skill,\s*loaded\)/);
  assert.match(source, /const displayedLoadedSkills = computed\(/);
  assert.match(source, /const displayedAvailableSkills = computed\(/);
  assert.match(source, /<QSwitch\s+:modelValue="true"[\s\S]*?@update:modelValue="setSkillLoaded\(skill, \$event\)"/);
  assert.match(source, /<QSwitch\s+:modelValue="false"[\s\S]*?@update:modelValue="setSkillLoaded\(skill, \$event\)"/);
  assert.match(source, /settings_skills_disable_action/);
  assert.match(source, /settings_skills_enable_action/);
  assert.match(source, /skills:\s*\{\s*enabled:\s*!!state\.skills\.enabled,\s*load:\s*parseSkillLoadText\(state\.skills\.load_text\)\s*\}/);

  const skillsSectionStart = source.indexOf("selectedSection.id === 'skills'");
  assert.notEqual(skillsSectionStart, -1, "skills section not found");
  const personaSectionStart = source.indexOf("selectedSection.id === 'persona'", skillsSectionStart);
  assert.notEqual(personaSectionStart, -1, "next settings section not found");
  const skillsSection = source.slice(skillsSectionStart, personaSectionStart);

  assert.doesNotMatch(skillsSection, /settings_skills_load_label/);
  assert.doesNotMatch(skillsSection, /<QTextarea[\s\S]*state\.skills\.load_text/);
  assert.match(skillsSection, /v-for="skill in displayedLoadedSkills"/);
  assert.match(skillsSection, /v-for="skill in displayedAvailableSkills"/);
});
