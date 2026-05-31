import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appNavListSource = new URL("../components/AppNavList.js", import.meta.url);
const appSidebarSource = new URL("../components/AppSidebar.js", import.meta.url);
const appMobileNavDrawerSource = new URL("../components/AppMobileNavDrawer.js", import.meta.url);
const slotsSource = new URL("../ext/slots/index.js", import.meta.url);

test("sidebar exposes a slot before the runtime nav link", async () => {
  const [navList, sidebar, mobileDrawer, slots] = await Promise.all([
    readFile(appNavListSource, "utf8"),
    readFile(appSidebarSource, "utf8"),
    readFile(appMobileNavDrawerSource, "utf8"),
    readFile(slotsSource, "utf8"),
  ]);

  assert.match(slots, /"sidebar\.before_runtime":\s*null/);

  assert.match(navList, /import\s+\{\s*computed\s*\}\s+from "vue";/);
  assert.match(navList, /import\s+\{\s*uiSlots\s*\}\s+from "\.\.\/ext\/slots";/);
  assert.match(navList, /const sidebarBeforeRuntimeSlot = computed\(\(\) => uiSlots\["sidebar\.before_runtime"\] \|\| null\);/);
  assert.match(navList, /v-if="shouldRenderBeforeRuntimeSlot\(item\)"/);
  assert.match(navList, /:is="sidebarBeforeRuntimeSlot"/);
  assert.match(navList, /:selectedEndpointItem="selectedEndpointItem"/);
  assert.match(navList, /:currentPath="currentPath"/);
  assert.match(navList, /:mobile="mobile"/);
  assert.match(navList, /:t="t"/);

  const slotIndex = navList.indexOf('v-if="shouldRenderBeforeRuntimeSlot(item)"');
  const linkIndex = navList.indexOf("<a", slotIndex);
  assert.ok(slotIndex >= 0, "runtime slot marker missing");
  assert.ok(linkIndex > slotIndex, "runtime nav link should render after the slot");

  for (const source of [sidebar, mobileDrawer]) {
    assert.match(source, /:selectedEndpointItem="selectedEndpointItem"/);
    assert.match(source, /:t="t"/);
  }
});
