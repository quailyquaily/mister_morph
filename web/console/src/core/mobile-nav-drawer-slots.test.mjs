import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const mobileDrawerSource = new URL("../components/AppMobileNavDrawer.js", import.meta.url);

test("mobile nav drawer renders the sidebar bottom-left extension slot", async () => {
  const source = await readFile(mobileDrawerSource, "utf8");

  assert.match(source, /import\s+\{\s*computed\s*\}\s+from "vue";/);
  assert.match(source, /import\s+\{\s*uiSlots\s*\}\s+from "\.\.\/ext\/slots";/);
  assert.match(source, /const sidebarBottomLeftSlot = computed\(\(\) => uiSlots\["sidebar\.bottom_left"\] \|\| null\);/);
  assert.match(source, /v-if="sidebarBottomLeftSlot"\s+class="sidebar-slot sidebar-slot-bottom-left"/);
  assert.match(source, /:is="sidebarBottomLeftSlot"/);
  assert.match(source, /:selectedEndpointItem="selectedEndpointItem"/);
  assert.match(source, /:currentPath="currentPath"/);
  assert.match(source, /:t="t"/);
});
