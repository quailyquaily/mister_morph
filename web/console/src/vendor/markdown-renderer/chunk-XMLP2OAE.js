import {
  AbstractMermaidTokenBuilder,
  CommonValueConverter,
  EmptyFileSystem,
  InfoGrammarGeneratedModule,
  MermaidGeneratedSharedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-WXRUUXZI.js";

// node_modules/.pnpm/@mermaid-js+parser@1.0.1/node_modules/@mermaid-js/parser/dist/chunks/mermaid-parser.core/chunk-EGIJ26TM.mjs
var _a;
var InfoTokenBuilder = (_a = class extends AbstractMermaidTokenBuilder {
  constructor() {
    super(["info", "showInfo"]);
  }
}, __name(_a, "InfoTokenBuilder"), _a);
var InfoModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new InfoTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new CommonValueConverter(), "ValueConverter")
  }
};
function createInfoServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Info = inject(
    createDefaultCoreModule({ shared }),
    InfoGrammarGeneratedModule,
    InfoModule
  );
  shared.ServiceRegistry.register(Info);
  return { shared, Info };
}
__name(createInfoServices, "createInfoServices");

export {
  InfoModule,
  createInfoServices
};
