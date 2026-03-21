import {
  StateDB,
  stateDiagram_default,
  stateRenderer_v3_unified_default,
  styles_default
} from "./chunk-QW5H44KI.js";
import "./chunk-GWDP642B.js";
import "./chunk-QHQSQCP3.js";
import "./chunk-C3WZUWM4.js";
import "./chunk-UDRACCLK.js";
import "./chunk-PACFXPIU.js";
import "./chunk-CVIT6PSU.js";
import "./chunk-SD42X4BU.js";
import "./chunk-H7ULSHMD.js";
import "./chunk-RE67LI6L.js";
import "./chunk-G3EO2CC6.js";
import "./chunk-YDWMGJMV.js";
import "./chunk-NR5OB4D2.js";
import "./chunk-Y5Y2VWTZ.js";
import {
  __name
} from "./chunk-BRRQQMRE.js";
import "./chunk-J36ZT73G.js";
import "./chunk-TBFEXUVH.js";
import "./chunk-NV3YER2Z.js";
import "./chunk-FQSBEHN3.js";

// node_modules/.pnpm/mermaid@11.13.0/node_modules/mermaid/dist/chunks/mermaid.core/stateDiagram-v2-FVOUBMTO.mjs
var diagram = {
  parser: stateDiagram_default,
  get db() {
    return new StateDB(2);
  },
  renderer: stateRenderer_v3_unified_default,
  styles: styles_default,
  init: /* @__PURE__ */ __name((cnf) => {
    if (!cnf.state) {
      cnf.state = {};
    }
    cnf.state.arrowMarkerAbsolute = cnf.arrowMarkerAbsolute;
  }, "init")
};
export {
  diagram
};
