import SidebarBottomLeft from "./SidebarBottomLeft";

const uiSlots = {
  "sidebar.before_runtime": null,
  "sidebar.bottom_left": SidebarBottomLeft || null,
};

export { uiSlots };
