import { normalizeTopicID } from "./chat-task-history.js";

function composerFileDraftKey(scope) {
  const endpointRef = String(scope?.endpointRef || "").trim();
  if (!endpointRef) {
    return "";
  }
  return `${endpointRef}\n${normalizeTopicID(scope?.topicID)}`;
}

function composerFileExtension(name) {
  const normalized = String(name || "").trim().toLowerCase();
  const index = normalized.lastIndexOf(".");
  return index >= 0 ? normalized.slice(index) : "";
}

function composerFileReferences(items) {
  return (Array.isArray(items) ? items : [])
    .filter((item) => String(item?.status || "").trim() === "ready")
    .map((item) => ({
      dir_name: String(item?.dirName || item?.dir_name || "").trim(),
      path: String(item?.path || "").trim(),
    }))
    .filter(
      (reference) =>
        reference.path &&
        (reference.dir_name === "workspace_dir" || reference.dir_name === "file_cache_dir")
    );
}

function buildComposerSubmission(options = {}) {
  const submittedFiles = (Array.isArray(options.files) ? options.files : []).filter(
    (item) => String(item?.status || "").trim() === "ready"
  );
  const fileReferences = composerFileReferences(submittedFiles);
  const requestBody = { task: String(options.task || "").trim() };
  const llmProfile = String(options.llmProfile || "").trim();
  const topicID = normalizeTopicID(options.topicID);
  const workspaceDir = String(options.workspaceDir || "").trim();
  if (llmProfile) {
    requestBody.llm_profile = llmProfile;
  }
  if (fileReferences.length > 0) {
    requestBody.file_references = fileReferences;
  }
  if (topicID) {
    requestBody.topic_id = topicID;
  }
  if (workspaceDir) {
    requestBody.workspace_dir = workspaceDir;
  }
  return { requestBody, submittedFiles, fileReferences };
}

export {
  buildComposerSubmission,
  composerFileDraftKey,
  composerFileExtension,
  composerFileReferences,
};
