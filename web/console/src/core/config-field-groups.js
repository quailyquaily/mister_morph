export const DEFAULT_MODEL_ADVANCED_CONFIG_GROUPS = [
  {
    id: "model-behavior",
    title: "Default model",
    note: "Low-frequency request and provider settings for the default profile.",
    fields: [
      { path: "llm.context_window_tokens", label: "Context window", type: "int" },
      { path: "llm.supports_image_parts", label: "Supports image parts", type: "string", placeholder: "auto, true, or false" },
      { path: "llm.headers", label: "HTTP headers", type: "json", wide: true, placeholder: "{}" },
      { path: "llm.cache_ttl", label: "Cache TTL", type: "string", placeholder: "off, short, long, or 5m" },
      { path: "llm.cache_key_prefix", label: "Cache key prefix", type: "string" },
      { path: "llm.request_timeout", label: "Request timeout", type: "string", placeholder: "90s" },
      { path: "llm.temperature", label: "Temperature", type: "float" },
      {
        path: "llm.reasoning_effort",
        label: "Reasoning effort",
        type: "select",
        options: ["", "none", "minimal", "low", "medium", "high", "max", "xhigh"],
        placeholder: "Provider default",
      },
      { path: "llm.reasoning_budget_tokens", label: "Reasoning budget tokens", type: "int" },
      {
        path: "llm.tools_emulation_mode",
        label: "Tools emulation",
        type: "select",
        options: ["off", "fallback", "force"],
      },
      { path: "llm.azure.deployment", label: "Azure deployment", type: "string" },
      { path: "llm.bedrock.aws_session_token", label: "Bedrock session token", type: "string", secret: true },
      { path: "llm.bedrock.aws_profile", label: "Bedrock AWS profile", type: "string" },
    ],
  },
];

export const LLM_SYSTEM_CONFIG_GROUPS = [
  {
    id: "image-model",
    title: "Image model",
    note: "Used by image_generate and image_edit. Empty fields use the supported default-model inheritance rules.",
    fields: [
      { path: "llm.image.provider", label: "Provider", type: "string" },
      { path: "llm.image.endpoint", label: "API base", type: "string" },
      { path: "llm.image.api_key", label: "API key", type: "string", secret: true },
      { path: "llm.image.model", label: "Model", type: "string" },
      { path: "llm.image.request_timeout", label: "Request timeout", type: "string", placeholder: "180s" },
      { path: "llm.image.options.openai", label: "OpenAI options", type: "json", wide: true },
      { path: "llm.image.options.gemini", label: "Gemini options", type: "json", wide: true },
      { path: "llm.image.options.cloudflare", label: "Cloudflare options", type: "json", wide: true },
    ],
  },
  {
    id: "model-routes",
    title: "Model routes",
    note: "Route main loop, addressing, awareness, think, and plan creation to named profiles.",
    fields: [
      { path: "llm.routes.main_loop", label: "Main loop", type: "json", wide: true, placeholder: "{}" },
      { path: "llm.routes.addressing", label: "Addressing", type: "json", wide: true, placeholder: "{}" },
      { path: "llm.routes.awareness", label: "Awareness", type: "json", wide: true, placeholder: "{}" },
      { path: "llm.routes.think", label: "Think", type: "json", wide: true, placeholder: "{}" },
      { path: "llm.routes.plan_create", label: "Plan creation", type: "json", wide: true, placeholder: "{}" },
    ],
  },
  {
    id: "execution-limits",
    title: "Execution limits",
    fields: [
      { path: "max_steps", label: "Maximum steps", type: "int" },
      { path: "parse_retries", label: "Parse retries", type: "int" },
      { path: "max_token_budget", label: "Maximum token budget", type: "int", note: "0 disables the token budget." },
      { path: "tool_repeat_limit", label: "Tool repeat limit", type: "int" },
      { path: "timeout", label: "Run timeout", type: "string", placeholder: "10m" },
    ],
  },
];

export const LLM_CONTEXT_CONFIG_GROUPS = [{
  id: "context",
  title: "Context compaction",
  fields: [
    { path: "context_compaction.enabled", label: "Context compaction", type: "bool" },
    { path: "context_compaction.trigger_ratio", label: "Compaction trigger ratio", type: "float" },
  ],
}];

export const TOOL_ADVANCED_CONFIG_GROUPS = {
  read_file: [{
    id: "read-file",
    title: "read_file",
    fields: [
      { path: "tools.read_file.max_bytes", label: "Read file maximum bytes", type: "int" },
      { path: "tools.read_file.deny_paths", label: "Read file denied paths", type: "string_list", wide: true },
    ],
  }],
  write_file: [{
    id: "write-file",
    title: "write_file",
    fields: [
      { path: "tools.write_file.max_bytes", label: "Write file maximum bytes", type: "int" },
    ],
  }],
  coder: [{
    id: "coder",
    title: "coder",
    fields: [
      { path: "tools.coder.path_extra", label: "Coder PATH additions", type: "string_list", wide: true },
    ],
  }],
  plan_create: [{
    id: "plan-create",
    title: "plan_create",
    fields: [
      { path: "tools.plan_create.max_steps", label: "Plan maximum steps", type: "int" },
    ],
  }],
  url_fetch: [{
    id: "url-fetch",
    title: "url_fetch",
    fields: [
      { path: "tools.url_fetch.timeout", label: "URL fetch timeout", type: "string" },
      { path: "tools.url_fetch.max_bytes", label: "URL fetch maximum bytes", type: "int" },
      { path: "tools.url_fetch.max_bytes_download", label: "Download maximum bytes", type: "int" },
    ],
  }],
  web_search: [{
    id: "web-search",
    title: "web_search",
    fields: [
      { path: "tools.web_search.base_url", label: "Web search base URL", type: "string", wide: true },
      { path: "tools.web_search.timeout", label: "Web search timeout", type: "string" },
      { path: "tools.web_search.max_results", label: "Web search maximum results", type: "int" },
    ],
  }],
  bash: [{
    id: "bash",
    title: "bash",
    fields: [
      { path: "tools.bash.timeout", label: "Bash timeout", type: "string" },
      { path: "tools.bash.max_output_bytes", label: "Bash maximum output bytes", type: "int" },
      { path: "tools.bash.deny_paths", label: "Bash denied paths", type: "string_list", wide: true },
      { path: "tools.bash.path_extra", label: "Bash PATH additions", type: "string_list", wide: true },
      { path: "tools.bash.injected_env_vars", label: "Bash injected environment", type: "json", wide: true },
      { path: "tools.bash.rewrite.enabled", label: "Bash command rewrite", type: "bool" },
      { path: "tools.bash.rewrite.binary", label: "Rewrite binary", type: "string" },
    ],
  }],
  powershell: [{
    id: "powershell",
    title: "powershell",
    fields: [
      { path: "tools.powershell.timeout", label: "PowerShell timeout", type: "string" },
      { path: "tools.powershell.max_output_bytes", label: "PowerShell maximum output bytes", type: "int" },
      { path: "tools.powershell.deny_paths", label: "PowerShell denied paths", type: "string_list", wide: true },
      { path: "tools.powershell.injected_env_vars", label: "PowerShell injected environment", type: "json", wide: true },
    ],
  }],
};

function channelFields(channel, options = {}) {
  const title = options.title || channel[0].toUpperCase() + channel.slice(1);
  const fields = [];
  if (options.baseURL) fields.push({ path: `${channel}.base_url`, label: "API base", type: "string", wide: true });
  if (options.webhook) {
    fields.push(
      { path: `${channel}.webhook_listen`, label: "Webhook listen address", type: "string" },
      { path: `${channel}.webhook_path`, label: "Webhook path", type: "string" },
    );
  }
  if (options.group !== false) {
    fields.push(
      { path: `${channel}.record_untriggered`, label: "Record untriggered group messages", type: "bool" },
      { path: `${channel}.addressing_confidence_threshold`, label: "Addressing confidence threshold", type: "float" },
      { path: `${channel}.addressing_interject_threshold`, label: "Addressing interject threshold", type: "float" },
    );
  }
  if (options.poll) fields.push({ path: `${channel}.poll_timeout`, label: "Poll timeout", type: "string" });
  fields.push(
    { path: `${channel}.task_timeout`, label: "Task timeout", type: "string" },
    { path: `${channel}.max_concurrency`, label: "Maximum concurrency", type: "int" },
    { path: `${channel}.serve_listen`, label: "Runtime API listen address", type: "string" },
  );
  return { id: channel, title: `${title} behavior`, fields };
}

export const CHANNEL_CONFIG_GROUPS = [
  channelFields("telegram", { title: "Telegram", poll: true }),
  channelFields("slack", { title: "Slack", baseURL: true }),
  channelFields("line", { title: "LINE", baseURL: true, webhook: true }),
  channelFields("lark", { title: "Lark", baseURL: true }),
  channelFields("mixin", { title: "Mixin", group: false }),
];

export const AUTOMATION_CONFIG_GROUPS = [
  {
    id: "automation",
    title: "Automation",
    note: "Heartbeat runs through the cron service. Both switches must be enabled for scheduled heartbeats.",
    fields: [
      { path: "heartbeat.enabled", label: "Heartbeat", type: "bool" },
      { path: "heartbeat.interval", label: "Heartbeat interval", type: "string", placeholder: "30m" },
      { path: "cron.enabled", label: "TODO scheduler", type: "bool" },
    ],
  },
];

export const SECURITY_CONFIG_GROUPS = [
  {
    id: "guard-storage",
    title: "Guard details",
    fields: [
      { path: "guard.dir_name", label: "Guard directory name", type: "string" },
      { path: "guard.redaction.patterns", label: "Additional redaction patterns", type: "json", wide: true },
      { path: "guard.audit.jsonl_path", label: "Audit JSONL path", type: "string", wide: true },
      { path: "guard.audit.rotate_max_bytes", label: "Audit rotation bytes", type: "int" },
    ],
  },
  {
    id: "administrators",
    title: "Administrators",
    fields: [
      { path: "admins", label: "Admin identities", type: "string_list", wide: true, note: "One protocol identity per line." },
    ],
  },
  {
    id: "secret-sources",
    title: "Secret sources",
    fields: [
      { path: "secrets.allow_profiles", label: "Allowed auth profiles", type: "string_list", wide: true },
      { path: "secrets.aws_secrets_manager.region", label: "AWS Secrets Manager region", type: "string" },
      { path: "secrets.aws_secrets_manager.profile", label: "AWS profile", type: "string" },
    ],
  },
];

export const SYSTEM_CONFIG_GROUPS = [
  {
    id: "logging",
    title: "Logging",
    fields: [
      { path: "logging.level", label: "Level", type: "string", placeholder: "debug, info, warn, or error" },
      { path: "logging.format", label: "Format", type: "string", placeholder: "text or json" },
    ],
  },
];

export const SYSTEM_UPDATE_CONFIG_GROUPS = [
  {
    id: "updates",
    title: "Updates",
    fields: [
      { path: "auto_update.enabled", label: "Automatic updates", type: "bool" },
    ],
  },
];

export const SYSTEM_ADVANCED_CONFIG_GROUPS = [
  {
    id: "logging-details",
    title: "Logging details",
    fields: [
      { path: "logging.add_source", label: "Include source location", type: "bool" },
      { path: "logging.file.dir", label: "Log directory", type: "string" },
      { path: "logging.file.max_age", label: "Log retention", type: "string" },
      { path: "logging.include_thoughts", label: "Include thoughts", type: "bool" },
      { path: "logging.include_tool_params", label: "Include tool parameters", type: "bool" },
      { path: "logging.include_skill_contents", label: "Include skill contents", type: "bool" },
      { path: "logging.max_thought_chars", label: "Maximum thought characters", type: "int" },
      { path: "logging.max_json_bytes", label: "Maximum JSON bytes", type: "int" },
      { path: "logging.max_string_value_chars", label: "Maximum string characters", type: "int" },
      { path: "logging.max_skill_content_chars", label: "Maximum skill content characters", type: "int" },
      { path: "logging.redact_keys", label: "Additional redacted keys", type: "string_list", wide: true },
    ],
  },
  {
    id: "paths-storage",
    title: "Paths and storage",
    fields: [
      { path: "workspace_dir", label: "Default workspace directory", type: "string", wide: true },
      { path: "file_state_dir", label: "State directory", type: "string", wide: true },
      { path: "file_cache_dir", label: "File cache directory", type: "string", wide: true },
      { path: "file_cache.max_age", label: "File cache maximum age", type: "string" },
      { path: "file_cache.max_files", label: "File cache maximum files", type: "int" },
      { path: "file_cache.max_total_bytes", label: "File cache maximum bytes", type: "int" },
      { path: "contacts.dir_name", label: "Contacts directory name", type: "string" },
      { path: "contacts.proactive.failure_cooldown", label: "Contact failure cooldown", type: "string" },
      { path: "tasks.dir_name", label: "Tasks directory name", type: "string" },
      { path: "tasks.persistence_targets", label: "Task persistence targets", type: "string_list", wide: true },
      { path: "tasks.rotate_max_bytes", label: "Task journal rotation bytes", type: "int" },
    ],
  },
  {
    id: "runtime-capacity",
    title: "Runtime capacity",
    fields: [
      { path: "server.max_queue", label: "Maximum queued tasks", type: "int" },
      { path: "bus.max_inflight", label: "Maximum in-flight bus messages", type: "int" },
      { path: "user_agent", label: "Outbound User-Agent", type: "string", wide: true },
    ],
  },
];

export const REMOTE_CONTROL_CONFIG_GROUPS = [
  {
    id: "incoming-control",
    title: "This Morph",
    note: "Allow another Morph Console or API client to control this Morph through /runtime. Leave the token empty to keep incoming remote control disabled.",
    fields: [
      {
        path: "server.auth_token",
        label: "Incoming access token",
        type: "string",
        secret: true,
        wide: true,
        note: "Clients send this value as a Bearer token. It is separate from Web Console sign-in.",
      },
    ],
  },
];

export const CONSOLE_DEPLOYMENT_CONFIG_GROUPS = [
  {
    id: "web-console-deployment",
    title: "Web Console",
    note: "Browser entry point and session settings. These do not authenticate Runtime API clients.",
    fields: [
      { path: "console.listen", label: "Listen address", type: "string" },
      { path: "console.base_path", label: "Base path", type: "string" },
      { path: "console.session_ttl", label: "Browser session lifetime", type: "string" },
      {
        path: "console.static_dir",
        label: "Static files directory",
        type: "string",
        wide: true,
        note: "Optional override for custom Web Console build files.",
      },
    ],
  },
];
