import { defineStore } from "pinia";

import { pinia } from "./pinia";

const LANGUAGE_STORAGE_KEY = "quail-language";

function normalizeLang(raw) {
  const value = String(raw || "").trim().toLowerCase();
  if (value.startsWith("zh")) {
    return "zh";
  }
  if (value.startsWith("ja")) {
    return "ja";
  }
  return "en";
}

const useLocaleStore = defineStore("locale", {
  state: () => ({
    lang: "en",
  }),
  actions: {
    setLanguage(lang) {
      this.lang = normalizeLang(lang);
      localStorage.setItem(LANGUAGE_STORAGE_KEY, this.lang);
    },
    hydrateLanguage() {
      const fromStorage = localStorage.getItem(LANGUAGE_STORAGE_KEY);
      if (fromStorage) {
        this.lang = normalizeLang(fromStorage);
        return;
      }
      this.lang = normalizeLang(navigator.language || "");
      localStorage.setItem(LANGUAGE_STORAGE_KEY, this.lang);
    },
    applyLanguageChange(item) {
      if (item && typeof item === "object" && "value" in item) {
        this.setLanguage(item.value);
        return;
      }
      this.setLanguage(item);
    },
  },
});

const localeState = useLocaleStore(pinia);

export { localeState, useLocaleStore };
