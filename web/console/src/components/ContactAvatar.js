import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

import { runtimeApiDownloadForEndpoint } from "../core/context";
import "./ContactAvatar.css";

const ContactAvatar = {
  props: {
    item: { type: Object, required: true },
    name: { type: String, default: "" },
    endpointRef: { type: String, required: true },
    size: { type: String, default: "list" },
  },
  setup(props) {
    const root = ref(null);
    const source = ref("");
    const failed = ref(false);
    const avatarPath = computed(() => String(props.item?.avatar_url || "").trim());
    const initial = computed(() => Array.from(String(props.name || "").trim())[0]?.toUpperCase() || "?");
    const avatarClass = computed(() => `contact-avatar is-${props.size === "detail" ? "detail" : "list"}`);

    let observer = null;
    let objectURL = "";
    let requestID = 0;

    function revokeObjectURL() {
      if (objectURL) {
        URL.revokeObjectURL(objectURL);
        objectURL = "";
      }
    }

    function reset() {
      requestID += 1;
      source.value = "";
      failed.value = false;
      revokeObjectURL();
    }

    async function load() {
      const endpointRef = String(props.endpointRef || "").trim();
      const path = avatarPath.value;
      if (!endpointRef || !path || source.value || failed.value) {
        return;
      }
      const currentRequestID = ++requestID;
      try {
        const blob = await runtimeApiDownloadForEndpoint(endpointRef, path, { cache: "force-cache" });
        if (currentRequestID !== requestID) {
          return;
        }
        revokeObjectURL();
        objectURL = URL.createObjectURL(blob);
        source.value = objectURL;
      } catch {
        if (currentRequestID === requestID) {
          failed.value = true;
        }
      }
    }

    function observe() {
      observer?.disconnect();
      observer = null;
      if (!avatarPath.value || !root.value) {
        return;
      }
      if (typeof IntersectionObserver === "undefined") {
        void load();
        return;
      }
      observer = new IntersectionObserver(
        (entries) => {
          if (!entries.some((entry) => entry.isIntersecting)) {
            return;
          }
          observer?.disconnect();
          observer = null;
          void load();
        },
        { rootMargin: "128px" }
      );
      observer.observe(root.value);
    }

    function handleImageError() {
      reset();
      failed.value = true;
    }

    onMounted(observe);
    onBeforeUnmount(() => {
      observer?.disconnect();
      reset();
    });
    watch(
      () => [props.endpointRef, avatarPath.value],
      () => {
        reset();
        queueMicrotask(observe);
      },
      { flush: "post" }
    );

    return { root, source, initial, avatarClass, handleImageError };
  },
  template: `
    <span ref="root" :class="avatarClass" aria-hidden="true">
      <img
        v-if="source"
        class="contact-avatar-image"
        :src="source"
        alt=""
        loading="lazy"
        decoding="async"
        @error="handleImageError"
      />
      <span v-else class="contact-avatar-fallback">{{ initial }}</span>
    </span>
  `,
};

export default ContactAvatar;
