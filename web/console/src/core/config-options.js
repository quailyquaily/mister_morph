export const IMAGE_PARTS_OPTIONS = [
  { title: "Auto", value: "" },
  { title: "Supported", value: "true" },
  { title: "Not supported", value: "false" },
];

export const CACHE_TTL_OPTIONS = [
  { title: "Default", value: "" },
  { title: "Off", value: "off" },
  { title: "Short (5 minutes)", value: "short" },
  { title: "Long (1 hour)", value: "long" },
];

export const HTTP_METHOD_OPTIONS = ["GET", "POST", "PUT", "PATCH", "DELETE"];
export const TASK_TARGET_OPTIONS = ["console", "telegram", "slack", "line", "lark", "mixin"];
