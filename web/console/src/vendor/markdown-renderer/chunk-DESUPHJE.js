import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  PieGrammarGeneratedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-WXRUUXZI.js";

// node_modules/.pnpm/@mermaid-js+parser@1.0.1/node_modules/@mermaid-js/parser/dist/chunks/mermaid-parser.core/chunk-XIRO2GV7.mjs
var _a;
var PieTokenBuilder = (_a = class extends AbstractMermaidTokenBuilder {
  constructor() {
    super(["pie", "showData"]);
  }
}, __name(_a, "PieTokenBuilder"), _a);
var _a2;
var PieValueConverter = (_a2 = class extends AbstractMermaidValueConverter {
  runCustomConverter(rule, input, _cstNode) {
    if (rule.name !== "PIE_SECTION_LABEL") {
      return void 0;
    }
    return input.replace(/"/g, "").trim();
  }
}, __name(_a2, "PieValueConverter"), _a2);
var PieModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new PieTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new PieValueConverter(), "ValueConverter")
  }
};
function createPieServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Pie = inject(
    createDefaultCoreModule({ shared }),
    PieGrammarGeneratedModule,
    PieModule
  );
  shared.ServiceRegistry.register(Pie);
  return { shared, Pie };
}
__name(createPieServices, "createPieServices");

export {
  PieModule,
  createPieServices
};
