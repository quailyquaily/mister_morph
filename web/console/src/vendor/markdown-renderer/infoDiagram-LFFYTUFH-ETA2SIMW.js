import {
  selectSvgElement
} from "./chunk-7ZQ22YFG.js";
import {
  parse
} from "./chunk-FIMOCYRW.js";
import "./chunk-MNVQ6IXA.js";
import "./chunk-LZV4ACDX.js";
import "./chunk-E733SPDA.js";
import "./chunk-XMLP2OAE.js";
import "./chunk-WQFXVUOS.js";
import "./chunk-DESUPHJE.js";
import "./chunk-Z4DSERFJ.js";
import "./chunk-WXRUUXZI.js";
import {
  configureSvgSize
} from "./chunk-Y5Y2VWTZ.js";
import {
  __name,
  log
} from "./chunk-BRRQQMRE.js";
import "./chunk-J36ZT73G.js";
import "./chunk-VFJUT2JB.js";
import "./chunk-PQTWO7OT.js";
import "./chunk-TBFEXUVH.js";
import "./chunk-NV3YER2Z.js";
import "./chunk-FQSBEHN3.js";

// node_modules/.pnpm/mermaid@11.13.0/node_modules/mermaid/dist/chunks/mermaid.core/infoDiagram-LFFYTUFH.mjs
var parser = {
  parse: /* @__PURE__ */ __name(async (input) => {
    const ast = await parse("info", input);
    log.debug(ast);
  }, "parse")
};
var DEFAULT_INFO_DB = {
  version: "11.13.0" + (true ? "" : "-tiny")
};
var getVersion = /* @__PURE__ */ __name(() => DEFAULT_INFO_DB.version, "getVersion");
var db = {
  getVersion
};
var draw = /* @__PURE__ */ __name((text, id, version) => {
  log.debug("rendering info diagram\n" + text);
  const svg = selectSvgElement(id);
  configureSvgSize(svg, 100, 400, true);
  const group = svg.append("g");
  group.append("text").attr("x", 100).attr("y", 40).attr("class", "version").attr("font-size", 32).style("text-anchor", "middle").text(`v${version}`);
}, "draw");
var renderer = { draw };
var diagram = {
  parser,
  db,
  renderer
};
export {
  diagram
};
