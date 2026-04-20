import vitesseLightTheme from "../assets/workspace-icons/vitesse-light-theme.json";

const svgModules = import.meta.glob("../assets/workspace-icons/*.svg", {
  eager: true,
  import: "default",
});

const iconURLs = Object.fromEntries(
  Object.entries(svgModules).map(([path, url]) => {
    const filename = path.split("/").pop() || "";
    return [filename.replace(/\.svg$/u, ""), url];
  })
);

function normalizeLookupMap(raw) {
  return Object.fromEntries(
    Object.entries(raw || {}).map(([key, value]) => [String(key).toLowerCase(), value])
  );
}

const iconDefinitions = vitesseLightTheme?.iconDefinitions || {};
const fileNames = normalizeLookupMap(vitesseLightTheme?.fileNames);
const fileExtensions = normalizeLookupMap(vitesseLightTheme?.fileExtensions);
const fallbackFileIcon = String(vitesseLightTheme?.file || "file");
const fallbackFolderIcon = String(vitesseLightTheme?.folder || "folder");
const fallbackFolderExpandedIcon = String(vitesseLightTheme?.folderExpanded || "folder__open");
const commonFolderIcons = {
  api: "folder_api",
  app: "folder_app",
  apps: "folder_app",
  assets: "folder_images",
  build: "folder_dist",
  client: "folder_client",
  cmd: "folder_command",
  component: "folder_components",
  components: "folder_components",
  config: "folder_config",
  configs: "folder_config",
  coverage: "folder_coverage",
  dist: "folder_dist",
  doc: "folder_docs",
  docs: "folder_docs",
  example: "folder_examples",
  examples: "folder_examples",
  icons: "folder_images",
  images: "folder_images",
  img: "folder_images",
  lib: "folder_library",
  libs: "folder_library",
  node_modules: "folder_node",
  package: "folder_packages",
  packages: "folder_packages",
  page: "folder_views",
  pages: "folder_views",
  public: "folder_public",
  route: "folder_routes",
  routes: "folder_routes",
  script: "folder_scripts",
  scripts: "folder_scripts",
  server: "folder_server",
  src: "folder_src",
  static: "folder_resource",
  style: "folder_styles",
  styles: "folder_styles",
  temp: "folder_temp",
  test: "folder_tests",
  tests: "folder_tests",
  tmp: "folder_temp",
  vendor: "folder_library",
  view: "folder_views",
  views: "folder_views",
};

function iconURLByName(iconName) {
  const key = String(iconName || "").trim();
  if (!key) {
    return iconURLs.file || "";
  }
  const definition = iconDefinitions[key];
  const filename = String(definition?.iconPath || "")
    .trim()
    .split("/")
    .pop()
    ?.replace(/\.svg$/u, "");
  if (filename && iconURLs[filename]) {
    return iconURLs[filename];
  }
  return iconURLs[key] || iconURLs.file || "";
}

function basenameLower(name) {
  return String(name || "").trim().toLowerCase();
}

function fileExtensionCandidates(name) {
  const lower = basenameLower(name);
  if (!lower) {
    return [];
  }
  const candidates = [lower];
  let dotIndex = lower.indexOf(".");
  while (dotIndex >= 0 && dotIndex < lower.length - 1) {
    candidates.push(lower.slice(dotIndex + 1));
    dotIndex = lower.indexOf(".", dotIndex + 1);
  }
  return [...new Set(candidates)];
}

function folderIconName(name, expanded) {
  const matched = commonFolderIcons[name];
  if (!matched) {
    return expanded ? fallbackFolderExpandedIcon : fallbackFolderIcon;
  }
  return expanded ? `${matched}__open` : matched;
}

export function workspaceTreeIcon(entry, expanded = false) {
  const name = basenameLower(entry?.name);
  if (!name) {
    return iconURLByName(entry?.is_dir ? fallbackFolderIcon : fallbackFileIcon);
  }

  if (entry?.is_dir) {
    return iconURLByName(folderIconName(name, expanded));
  }

  if (fileNames[name]) {
    return iconURLByName(fileNames[name]);
  }

  for (const candidate of fileExtensionCandidates(name)) {
    if (fileExtensions[candidate]) {
      return iconURLByName(fileExtensions[candidate]);
    }
  }

  return iconURLByName(fallbackFileIcon);
}
