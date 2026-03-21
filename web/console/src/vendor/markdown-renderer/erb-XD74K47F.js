import {
  ruby_default
} from "./chunk-TGCYJ6LN.js";
import "./chunk-D2F5PDX5.js";
import "./chunk-THSXUYUD.js";
import "./chunk-VSC2TGU6.js";
import "./chunk-WSFIEG6Y.js";
import "./chunk-Q65SOB3R.js";
import "./chunk-3TPYRNBH.js";
import "./chunk-UTTWQ5HT.js";
import "./chunk-64CCKEM7.js";
import "./chunk-DQ7Z3GKK.js";
import "./chunk-4BV6QTFM.js";
import "./chunk-ZR63U53R.js";
import "./chunk-LKBY5WTO.js";
import "./chunk-INCRNP6E.js";
import "./chunk-HIISYYMZ.js";
import "./chunk-2DQ5PD4O.js";
import {
  html_default
} from "./chunk-3BJHUDMM.js";
import "./chunk-PBM3DDNY.js";
import "./chunk-VZOCVPTY.js";
import "./chunk-FQSBEHN3.js";

// node_modules/.pnpm/@shikijs+langs@4.0.2/node_modules/@shikijs/langs/dist/erb.mjs
var lang = Object.freeze(JSON.parse('{"displayName":"ERB","fileTypes":["erb","rhtml","html.erb"],"injections":{"text.html.erb - (meta.embedded.block.erb | meta.embedded.line.erb | comment)":{"patterns":[{"begin":"^(\\\\s*)(?=<%+#(?![^%]*%>))","beginCaptures":{"0":{"name":"punctuation.whitespace.comment.leading.erb"}},"end":"(?!\\\\G)(\\\\s*$\\\\n)?","endCaptures":{"0":{"name":"punctuation.whitespace.comment.trailing.erb"}},"patterns":[{"include":"#comment"}]},{"begin":"^(\\\\s*)(?=<%(?![^%]*%>))","beginCaptures":{"0":{"name":"punctuation.whitespace.embedded.leading.erb"}},"end":"(?!\\\\G)(\\\\s*$\\\\n)?","endCaptures":{"0":{"name":"punctuation.whitespace.embedded.trailing.erb"}},"patterns":[{"include":"#tags"}]},{"include":"#comment"},{"include":"#tags"}]}},"name":"erb","patterns":[{"include":"text.html.basic"}],"repository":{"comment":{"patterns":[{"begin":"<%+#","beginCaptures":{"0":{"name":"punctuation.definition.comment.begin.erb"}},"end":"%>","endCaptures":{"0":{"name":"punctuation.definition.comment.end.erb"}},"name":"comment.block.erb"}]},"tags":{"patterns":[{"begin":"<%+(?!>)[-=]?(?![^%]*%>)","beginCaptures":{"0":{"name":"punctuation.section.embedded.begin.erb"}},"contentName":"source.ruby","end":"(-?%)>","endCaptures":{"0":{"name":"punctuation.section.embedded.end.erb"},"1":{"name":"source.ruby"}},"name":"meta.embedded.block.erb","patterns":[{"captures":{"1":{"name":"punctuation.definition.comment.erb"}},"match":"(#).*?(?=-?%>)","name":"comment.line.number-sign.erb"},{"include":"source.ruby"}]},{"begin":"<%+(?!>)[-=]?","beginCaptures":{"0":{"name":"punctuation.section.embedded.begin.erb"}},"contentName":"source.ruby","end":"(-?%)>","endCaptures":{"0":{"name":"punctuation.section.embedded.end.erb"},"1":{"name":"source.ruby"}},"name":"meta.embedded.line.erb","patterns":[{"captures":{"1":{"name":"punctuation.definition.comment.erb"}},"match":"(#).*?(?=-?%>)","name":"comment.line.number-sign.erb"},{"include":"source.ruby"}]}]}},"scopeName":"text.html.erb","embeddedLangs":["html","ruby"]}'));
var erb_default = [
  ...html_default,
  ...ruby_default,
  lang
];
export {
  erb_default as default
};
