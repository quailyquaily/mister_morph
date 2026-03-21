import {
  getConfig2
} from "./chunk-Y5Y2VWTZ.js";
import {
  __name
} from "./chunk-BRRQQMRE.js";
import {
  select_default
} from "./chunk-J36ZT73G.js";

// node_modules/.pnpm/mermaid@11.13.0/node_modules/mermaid/dist/chunks/mermaid.core/chunk-HHEYEP7N.mjs
var selectSvgElement = /* @__PURE__ */ __name((id) => {
  const { securityLevel } = getConfig2();
  let root = select_default("body");
  if (securityLevel === "sandbox") {
    const sandboxElement = select_default(`#i${id}`);
    const doc = sandboxElement.node()?.contentDocument ?? document;
    root = select_default(doc.body);
  }
  const svg = root.select(`#${id}`);
  return svg;
}, "selectSvgElement");

export {
  selectSvgElement
};
