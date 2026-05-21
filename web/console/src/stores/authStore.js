import { computed } from "vue";
import { defineStore } from "pinia";

import { pinia } from "./pinia";

const AUTH_STORAGE_KEY = "mistermorph_console_auth_v1";

const useAuthStore = defineStore("auth", {
  state: () => ({
    token: "",
    expiresAt: "",
    account: "console",
  }),
  actions: {
    save() {
      localStorage.setItem(
        AUTH_STORAGE_KEY,
        JSON.stringify({
          token: this.token,
          expiresAt: this.expiresAt,
          account: this.account,
        })
      );
    },
    clear() {
      this.token = "";
      this.expiresAt = "";
      this.account = "console";
      localStorage.removeItem(AUTH_STORAGE_KEY);
    },
    hydrate() {
      const raw = localStorage.getItem(AUTH_STORAGE_KEY);
      if (!raw) {
        return;
      }
      try {
        const parsed = JSON.parse(raw);
        this.token = typeof parsed.token === "string" ? parsed.token : "";
        this.expiresAt = typeof parsed.expiresAt === "string" ? parsed.expiresAt : "";
        this.account = typeof parsed.account === "string" ? parsed.account : "console";
      } catch {
        this.clear();
      }
    },
  },
});

const authState = useAuthStore(pinia);
const authValid = computed(() => {
  if (!authState.token || !authState.expiresAt) {
    return false;
  }
  const ts = new Date(authState.expiresAt).getTime();
  if (!Number.isFinite(ts)) {
    return false;
  }
  return ts > Date.now();
});

export { authState, authValid, useAuthStore };
