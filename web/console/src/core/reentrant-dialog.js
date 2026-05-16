import { nextTick } from "vue";

export async function openReentrantDialog(openRef) {
  if (!openRef || typeof openRef !== "object") {
    return;
  }
  if (openRef.value) {
    openRef.value = false;
    await nextTick();
  }
  openRef.value = true;
}
