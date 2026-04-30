import { computed, nextTick, ref, watch } from "vue";

import ArtifactPreviewCard from "./ArtifactPreviewCard";
import MarkdownContent from "./MarkdownContent";

function parseArtifactFields(raw) {
  const fields = {};
  for (const line of String(raw || "").split(/\r?\n/u)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }
    const idx = trimmed.indexOf("=");
    if (idx <= 0) {
      continue;
    }
    const key = trimmed.slice(0, idx).trim().toLowerCase();
    let value = trimmed.slice(idx + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }
    if (key) {
      fields[key] = value;
    }
  }
  if (!fields.path || !fields.dir_name) {
    return null;
  }
  return fields;
}

function splitArtifactSegments(source) {
  const text = String(source || "");
  if (!text) {
    return [];
  }
  const segments = [];
  const re = /(^|\n)(`{3,}|~{3,})[ \t]*artifact[^\n]*\n([\s\S]*?)\n\2[ \t]*(?=\n|$)/gu;
  let lastIndex = 0;
  let index = 0;
  for (const match of text.matchAll(re)) {
    const prefix = match[1] || "";
    const blockStart = match.index + prefix.length;
    const blockEnd = match.index + match[0].length;
    const before = text.slice(lastIndex, blockStart);
    if (before.trim()) {
      segments.push({
        key: `markdown:${index}`,
        kind: "markdown",
        source: before,
      });
      index += 1;
    }
    const artifact = parseArtifactFields(match[3]);
    if (artifact) {
      segments.push({
        key: `artifact:${index}:${artifact.path}`,
        kind: "artifact",
        artifact,
      });
    } else {
      segments.push({
        key: `markdown:${index}`,
        kind: "markdown",
        source: text.slice(blockStart, blockEnd),
      });
    }
    index += 1;
    lastIndex = blockEnd;
  }
  const rest = text.slice(lastIndex);
  if (rest.trim()) {
    segments.push({
      key: `markdown:${index}`,
      kind: "markdown",
      source: rest,
    });
  }
  return segments;
}

const ChatRichContent = {
  components: {
    ArtifactPreviewCard,
    MarkdownContent,
  },
  emits: ["rendered"],
  props: {
    source: {
      type: String,
      default: "",
    },
    format: {
      type: String,
      default: "auto",
    },
    theme: {
      type: String,
      default: "paper",
    },
    endpointRef: {
      type: String,
      default: "",
    },
    fallbackTopicId: {
      type: String,
      default: "",
    },
    autoPreview: {
      type: Boolean,
      default: false,
    },
  },
  setup(props, { emit }) {
    const renderedMarkdown = ref({});
    const segments = computed(() => splitArtifactSegments(props.source));
    const markdownKeys = computed(() =>
      segments.value.filter((segment) => segment.kind === "markdown").map((segment) => segment.key)
    );

    function emitRenderedIfReady() {
      const keys = markdownKeys.value;
      if (keys.length === 0 || keys.every((key) => renderedMarkdown.value[key] === true)) {
        emit("rendered");
      }
    }

    function markMarkdownRendered(key) {
      const normalizedKey = String(key || "").trim();
      if (!normalizedKey) {
        return;
      }
      renderedMarkdown.value = {
        ...renderedMarkdown.value,
        [normalizedKey]: true,
      };
      emitRenderedIfReady();
    }

    watch(
      () => props.source,
      () => {
        renderedMarkdown.value = {};
        void nextTick(emitRenderedIfReady);
      },
      { immediate: true }
    );

    return {
      segments,
      markMarkdownRendered,
    };
  },
  template: `
    <div class="chat-rich-content">
      <template v-for="segment in segments" :key="segment.key">
        <MarkdownContent
          v-if="segment.kind === 'markdown'"
          class="chat-rich-markdown"
          :source="segment.source"
          :format="format"
          :theme="theme"
          @rendered="markMarkdownRendered(segment.key)"
        />
        <ArtifactPreviewCard
          v-else-if="segment.kind === 'artifact'"
          :artifact="segment.artifact"
          :endpoint-ref="endpointRef"
          :fallback-topic-id="fallbackTopicId"
          :auto-preview="autoPreview"
        />
      </template>
    </div>
  `,
};

export default ChatRichContent;
