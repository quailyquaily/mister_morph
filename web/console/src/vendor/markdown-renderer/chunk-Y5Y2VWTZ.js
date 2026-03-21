import {
  __export,
  __name,
  log,
  setLogLevel
} from "./chunk-BRRQQMRE.js";
import {
  purify
} from "./chunk-NV3YER2Z.js";

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/utils/channel.js
var Channel = {
  /* CLAMP */
  min: {
    r: 0,
    g: 0,
    b: 0,
    s: 0,
    l: 0,
    a: 0
  },
  max: {
    r: 255,
    g: 255,
    b: 255,
    h: 360,
    s: 100,
    l: 100,
    a: 1
  },
  clamp: {
    r: (r) => r >= 255 ? 255 : r < 0 ? 0 : r,
    g: (g) => g >= 255 ? 255 : g < 0 ? 0 : g,
    b: (b) => b >= 255 ? 255 : b < 0 ? 0 : b,
    h: (h) => h % 360,
    s: (s) => s >= 100 ? 100 : s < 0 ? 0 : s,
    l: (l) => l >= 100 ? 100 : l < 0 ? 0 : l,
    a: (a) => a >= 1 ? 1 : a < 0 ? 0 : a
  },
  /* CONVERSION */
  //SOURCE: https://planetcalc.com/7779
  toLinear: (c) => {
    const n = c / 255;
    return c > 0.03928 ? Math.pow((n + 0.055) / 1.055, 2.4) : n / 12.92;
  },
  //SOURCE: https://gist.github.com/mjackson/5311256
  hue2rgb: (p, q, t) => {
    if (t < 0)
      t += 1;
    if (t > 1)
      t -= 1;
    if (t < 1 / 6)
      return p + (q - p) * 6 * t;
    if (t < 1 / 2)
      return q;
    if (t < 2 / 3)
      return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  },
  hsl2rgb: ({ h, s, l }, channel2) => {
    if (!s)
      return l * 2.55;
    h /= 360;
    s /= 100;
    l /= 100;
    const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
    const p = 2 * l - q;
    switch (channel2) {
      case "r":
        return Channel.hue2rgb(p, q, h + 1 / 3) * 255;
      case "g":
        return Channel.hue2rgb(p, q, h) * 255;
      case "b":
        return Channel.hue2rgb(p, q, h - 1 / 3) * 255;
    }
  },
  rgb2hsl: ({ r, g, b }, channel2) => {
    r /= 255;
    g /= 255;
    b /= 255;
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const l = (max + min) / 2;
    if (channel2 === "l")
      return l * 100;
    if (max === min)
      return 0;
    const d = max - min;
    const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (channel2 === "s")
      return s * 100;
    switch (max) {
      case r:
        return ((g - b) / d + (g < b ? 6 : 0)) * 60;
      case g:
        return ((b - r) / d + 2) * 60;
      case b:
        return ((r - g) / d + 4) * 60;
      default:
        return -1;
    }
  }
};
var channel_default = Channel;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/utils/lang.js
var Lang = {
  /* API */
  clamp: (number, lower, upper) => {
    if (lower > upper)
      return Math.min(lower, Math.max(upper, number));
    return Math.min(upper, Math.max(lower, number));
  },
  round: (number) => {
    return Math.round(number * 1e10) / 1e10;
  }
};
var lang_default = Lang;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/utils/unit.js
var Unit = {
  /* API */
  dec2hex: (dec) => {
    const hex = Math.round(dec).toString(16);
    return hex.length > 1 ? hex : `0${hex}`;
  }
};
var unit_default = Unit;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/utils/index.js
var Utils = {
  channel: channel_default,
  lang: lang_default,
  unit: unit_default
};
var utils_default = Utils;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/constants.js
var DEC2HEX = {};
for (let i = 0; i <= 255; i++)
  DEC2HEX[i] = utils_default.unit.dec2hex(i);
var TYPE = {
  ALL: 0,
  RGB: 1,
  HSL: 2
};

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/channels/type.js
var Type = class {
  constructor() {
    this.type = TYPE.ALL;
  }
  /* API */
  get() {
    return this.type;
  }
  set(type) {
    if (this.type && this.type !== type)
      throw new Error("Cannot change both RGB and HSL channels at the same time");
    this.type = type;
  }
  reset() {
    this.type = TYPE.ALL;
  }
  is(type) {
    return this.type === type;
  }
};
var type_default = Type;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/channels/index.js
var Channels = class {
  /* CONSTRUCTOR */
  constructor(data, color) {
    this.color = color;
    this.changed = false;
    this.data = data;
    this.type = new type_default();
  }
  /* API */
  set(data, color) {
    this.color = color;
    this.changed = false;
    this.data = data;
    this.type.type = TYPE.ALL;
    return this;
  }
  /* HELPERS */
  _ensureHSL() {
    const data = this.data;
    const { h, s, l } = data;
    if (h === void 0)
      data.h = utils_default.channel.rgb2hsl(data, "h");
    if (s === void 0)
      data.s = utils_default.channel.rgb2hsl(data, "s");
    if (l === void 0)
      data.l = utils_default.channel.rgb2hsl(data, "l");
  }
  _ensureRGB() {
    const data = this.data;
    const { r, g, b } = data;
    if (r === void 0)
      data.r = utils_default.channel.hsl2rgb(data, "r");
    if (g === void 0)
      data.g = utils_default.channel.hsl2rgb(data, "g");
    if (b === void 0)
      data.b = utils_default.channel.hsl2rgb(data, "b");
  }
  /* GETTERS */
  get r() {
    const data = this.data;
    const r = data.r;
    if (!this.type.is(TYPE.HSL) && r !== void 0)
      return r;
    this._ensureHSL();
    return utils_default.channel.hsl2rgb(data, "r");
  }
  get g() {
    const data = this.data;
    const g = data.g;
    if (!this.type.is(TYPE.HSL) && g !== void 0)
      return g;
    this._ensureHSL();
    return utils_default.channel.hsl2rgb(data, "g");
  }
  get b() {
    const data = this.data;
    const b = data.b;
    if (!this.type.is(TYPE.HSL) && b !== void 0)
      return b;
    this._ensureHSL();
    return utils_default.channel.hsl2rgb(data, "b");
  }
  get h() {
    const data = this.data;
    const h = data.h;
    if (!this.type.is(TYPE.RGB) && h !== void 0)
      return h;
    this._ensureRGB();
    return utils_default.channel.rgb2hsl(data, "h");
  }
  get s() {
    const data = this.data;
    const s = data.s;
    if (!this.type.is(TYPE.RGB) && s !== void 0)
      return s;
    this._ensureRGB();
    return utils_default.channel.rgb2hsl(data, "s");
  }
  get l() {
    const data = this.data;
    const l = data.l;
    if (!this.type.is(TYPE.RGB) && l !== void 0)
      return l;
    this._ensureRGB();
    return utils_default.channel.rgb2hsl(data, "l");
  }
  get a() {
    return this.data.a;
  }
  /* SETTERS */
  set r(r) {
    this.type.set(TYPE.RGB);
    this.changed = true;
    this.data.r = r;
  }
  set g(g) {
    this.type.set(TYPE.RGB);
    this.changed = true;
    this.data.g = g;
  }
  set b(b) {
    this.type.set(TYPE.RGB);
    this.changed = true;
    this.data.b = b;
  }
  set h(h) {
    this.type.set(TYPE.HSL);
    this.changed = true;
    this.data.h = h;
  }
  set s(s) {
    this.type.set(TYPE.HSL);
    this.changed = true;
    this.data.s = s;
  }
  set l(l) {
    this.type.set(TYPE.HSL);
    this.changed = true;
    this.data.l = l;
  }
  set a(a) {
    this.changed = true;
    this.data.a = a;
  }
};
var channels_default = Channels;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/channels/reusable.js
var channels = new channels_default({ r: 0, g: 0, b: 0, a: 0 }, "transparent");
var reusable_default = channels;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/color/hex.js
var Hex = {
  /* VARIABLES */
  re: /^#((?:[a-f0-9]{2}){2,4}|[a-f0-9]{3})$/i,
  /* API */
  parse: (color) => {
    if (color.charCodeAt(0) !== 35)
      return;
    const match = color.match(Hex.re);
    if (!match)
      return;
    const hex = match[1];
    const dec = parseInt(hex, 16);
    const length = hex.length;
    const hasAlpha = length % 4 === 0;
    const isFullLength = length > 4;
    const multiplier = isFullLength ? 1 : 17;
    const bits = isFullLength ? 8 : 4;
    const bitsOffset = hasAlpha ? 0 : -1;
    const mask = isFullLength ? 255 : 15;
    return reusable_default.set({
      r: (dec >> bits * (bitsOffset + 3) & mask) * multiplier,
      g: (dec >> bits * (bitsOffset + 2) & mask) * multiplier,
      b: (dec >> bits * (bitsOffset + 1) & mask) * multiplier,
      a: hasAlpha ? (dec & mask) * multiplier / 255 : 1
    }, color);
  },
  stringify: (channels2) => {
    const { r, g, b, a } = channels2;
    if (a < 1) {
      return `#${DEC2HEX[Math.round(r)]}${DEC2HEX[Math.round(g)]}${DEC2HEX[Math.round(b)]}${DEC2HEX[Math.round(a * 255)]}`;
    } else {
      return `#${DEC2HEX[Math.round(r)]}${DEC2HEX[Math.round(g)]}${DEC2HEX[Math.round(b)]}`;
    }
  }
};
var hex_default = Hex;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/color/hsl.js
var HSL = {
  /* VARIABLES */
  re: /^hsla?\(\s*?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e-?\d+)?(?:deg|grad|rad|turn)?)\s*?(?:,|\s)\s*?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e-?\d+)?%)\s*?(?:,|\s)\s*?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e-?\d+)?%)(?:\s*?(?:,|\/)\s*?\+?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e-?\d+)?(%)?))?\s*?\)$/i,
  hueRe: /^(.+?)(deg|grad|rad|turn)$/i,
  /* HELPERS */
  _hue2deg: (hue) => {
    const match = hue.match(HSL.hueRe);
    if (match) {
      const [, number, unit] = match;
      switch (unit) {
        case "grad":
          return utils_default.channel.clamp.h(parseFloat(number) * 0.9);
        case "rad":
          return utils_default.channel.clamp.h(parseFloat(number) * 180 / Math.PI);
        case "turn":
          return utils_default.channel.clamp.h(parseFloat(number) * 360);
      }
    }
    return utils_default.channel.clamp.h(parseFloat(hue));
  },
  /* API */
  parse: (color) => {
    const charCode = color.charCodeAt(0);
    if (charCode !== 104 && charCode !== 72)
      return;
    const match = color.match(HSL.re);
    if (!match)
      return;
    const [, h, s, l, a, isAlphaPercentage] = match;
    return reusable_default.set({
      h: HSL._hue2deg(h),
      s: utils_default.channel.clamp.s(parseFloat(s)),
      l: utils_default.channel.clamp.l(parseFloat(l)),
      a: a ? utils_default.channel.clamp.a(isAlphaPercentage ? parseFloat(a) / 100 : parseFloat(a)) : 1
    }, color);
  },
  stringify: (channels2) => {
    const { h, s, l, a } = channels2;
    if (a < 1) {
      return `hsla(${utils_default.lang.round(h)}, ${utils_default.lang.round(s)}%, ${utils_default.lang.round(l)}%, ${a})`;
    } else {
      return `hsl(${utils_default.lang.round(h)}, ${utils_default.lang.round(s)}%, ${utils_default.lang.round(l)}%)`;
    }
  }
};
var hsl_default = HSL;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/color/keyword.js
var Keyword = {
  /* VARIABLES */
  colors: {
    aliceblue: "#f0f8ff",
    antiquewhite: "#faebd7",
    aqua: "#00ffff",
    aquamarine: "#7fffd4",
    azure: "#f0ffff",
    beige: "#f5f5dc",
    bisque: "#ffe4c4",
    black: "#000000",
    blanchedalmond: "#ffebcd",
    blue: "#0000ff",
    blueviolet: "#8a2be2",
    brown: "#a52a2a",
    burlywood: "#deb887",
    cadetblue: "#5f9ea0",
    chartreuse: "#7fff00",
    chocolate: "#d2691e",
    coral: "#ff7f50",
    cornflowerblue: "#6495ed",
    cornsilk: "#fff8dc",
    crimson: "#dc143c",
    cyanaqua: "#00ffff",
    darkblue: "#00008b",
    darkcyan: "#008b8b",
    darkgoldenrod: "#b8860b",
    darkgray: "#a9a9a9",
    darkgreen: "#006400",
    darkgrey: "#a9a9a9",
    darkkhaki: "#bdb76b",
    darkmagenta: "#8b008b",
    darkolivegreen: "#556b2f",
    darkorange: "#ff8c00",
    darkorchid: "#9932cc",
    darkred: "#8b0000",
    darksalmon: "#e9967a",
    darkseagreen: "#8fbc8f",
    darkslateblue: "#483d8b",
    darkslategray: "#2f4f4f",
    darkslategrey: "#2f4f4f",
    darkturquoise: "#00ced1",
    darkviolet: "#9400d3",
    deeppink: "#ff1493",
    deepskyblue: "#00bfff",
    dimgray: "#696969",
    dimgrey: "#696969",
    dodgerblue: "#1e90ff",
    firebrick: "#b22222",
    floralwhite: "#fffaf0",
    forestgreen: "#228b22",
    fuchsia: "#ff00ff",
    gainsboro: "#dcdcdc",
    ghostwhite: "#f8f8ff",
    gold: "#ffd700",
    goldenrod: "#daa520",
    gray: "#808080",
    green: "#008000",
    greenyellow: "#adff2f",
    grey: "#808080",
    honeydew: "#f0fff0",
    hotpink: "#ff69b4",
    indianred: "#cd5c5c",
    indigo: "#4b0082",
    ivory: "#fffff0",
    khaki: "#f0e68c",
    lavender: "#e6e6fa",
    lavenderblush: "#fff0f5",
    lawngreen: "#7cfc00",
    lemonchiffon: "#fffacd",
    lightblue: "#add8e6",
    lightcoral: "#f08080",
    lightcyan: "#e0ffff",
    lightgoldenrodyellow: "#fafad2",
    lightgray: "#d3d3d3",
    lightgreen: "#90ee90",
    lightgrey: "#d3d3d3",
    lightpink: "#ffb6c1",
    lightsalmon: "#ffa07a",
    lightseagreen: "#20b2aa",
    lightskyblue: "#87cefa",
    lightslategray: "#778899",
    lightslategrey: "#778899",
    lightsteelblue: "#b0c4de",
    lightyellow: "#ffffe0",
    lime: "#00ff00",
    limegreen: "#32cd32",
    linen: "#faf0e6",
    magenta: "#ff00ff",
    maroon: "#800000",
    mediumaquamarine: "#66cdaa",
    mediumblue: "#0000cd",
    mediumorchid: "#ba55d3",
    mediumpurple: "#9370db",
    mediumseagreen: "#3cb371",
    mediumslateblue: "#7b68ee",
    mediumspringgreen: "#00fa9a",
    mediumturquoise: "#48d1cc",
    mediumvioletred: "#c71585",
    midnightblue: "#191970",
    mintcream: "#f5fffa",
    mistyrose: "#ffe4e1",
    moccasin: "#ffe4b5",
    navajowhite: "#ffdead",
    navy: "#000080",
    oldlace: "#fdf5e6",
    olive: "#808000",
    olivedrab: "#6b8e23",
    orange: "#ffa500",
    orangered: "#ff4500",
    orchid: "#da70d6",
    palegoldenrod: "#eee8aa",
    palegreen: "#98fb98",
    paleturquoise: "#afeeee",
    palevioletred: "#db7093",
    papayawhip: "#ffefd5",
    peachpuff: "#ffdab9",
    peru: "#cd853f",
    pink: "#ffc0cb",
    plum: "#dda0dd",
    powderblue: "#b0e0e6",
    purple: "#800080",
    rebeccapurple: "#663399",
    red: "#ff0000",
    rosybrown: "#bc8f8f",
    royalblue: "#4169e1",
    saddlebrown: "#8b4513",
    salmon: "#fa8072",
    sandybrown: "#f4a460",
    seagreen: "#2e8b57",
    seashell: "#fff5ee",
    sienna: "#a0522d",
    silver: "#c0c0c0",
    skyblue: "#87ceeb",
    slateblue: "#6a5acd",
    slategray: "#708090",
    slategrey: "#708090",
    snow: "#fffafa",
    springgreen: "#00ff7f",
    tan: "#d2b48c",
    teal: "#008080",
    thistle: "#d8bfd8",
    transparent: "#00000000",
    turquoise: "#40e0d0",
    violet: "#ee82ee",
    wheat: "#f5deb3",
    white: "#ffffff",
    whitesmoke: "#f5f5f5",
    yellow: "#ffff00",
    yellowgreen: "#9acd32"
  },
  /* API */
  parse: (color) => {
    color = color.toLowerCase();
    const hex = Keyword.colors[color];
    if (!hex)
      return;
    return hex_default.parse(hex);
  },
  stringify: (channels2) => {
    const hex = hex_default.stringify(channels2);
    for (const name in Keyword.colors) {
      if (Keyword.colors[name] === hex)
        return name;
    }
    return;
  }
};
var keyword_default = Keyword;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/color/rgb.js
var RGB = {
  /* VARIABLES */
  re: /^rgba?\(\s*?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e\d+)?(%?))\s*?(?:,|\s)\s*?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e\d+)?(%?))\s*?(?:,|\s)\s*?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e\d+)?(%?))(?:\s*?(?:,|\/)\s*?\+?(-?(?:\d+(?:\.\d+)?|(?:\.\d+))(?:e\d+)?(%?)))?\s*?\)$/i,
  /* API */
  parse: (color) => {
    const charCode = color.charCodeAt(0);
    if (charCode !== 114 && charCode !== 82)
      return;
    const match = color.match(RGB.re);
    if (!match)
      return;
    const [, r, isRedPercentage, g, isGreenPercentage, b, isBluePercentage, a, isAlphaPercentage] = match;
    return reusable_default.set({
      r: utils_default.channel.clamp.r(isRedPercentage ? parseFloat(r) * 2.55 : parseFloat(r)),
      g: utils_default.channel.clamp.g(isGreenPercentage ? parseFloat(g) * 2.55 : parseFloat(g)),
      b: utils_default.channel.clamp.b(isBluePercentage ? parseFloat(b) * 2.55 : parseFloat(b)),
      a: a ? utils_default.channel.clamp.a(isAlphaPercentage ? parseFloat(a) / 100 : parseFloat(a)) : 1
    }, color);
  },
  stringify: (channels2) => {
    const { r, g, b, a } = channels2;
    if (a < 1) {
      return `rgba(${utils_default.lang.round(r)}, ${utils_default.lang.round(g)}, ${utils_default.lang.round(b)}, ${utils_default.lang.round(a)})`;
    } else {
      return `rgb(${utils_default.lang.round(r)}, ${utils_default.lang.round(g)}, ${utils_default.lang.round(b)})`;
    }
  }
};
var rgb_default = RGB;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/color/index.js
var Color = {
  /* VARIABLES */
  format: {
    keyword: keyword_default,
    hex: hex_default,
    rgb: rgb_default,
    rgba: rgb_default,
    hsl: hsl_default,
    hsla: hsl_default
  },
  /* API */
  parse: (color) => {
    if (typeof color !== "string")
      return color;
    const channels2 = hex_default.parse(color) || rgb_default.parse(color) || hsl_default.parse(color) || keyword_default.parse(color);
    if (channels2)
      return channels2;
    throw new Error(`Unsupported color format: "${color}"`);
  },
  stringify: (channels2) => {
    if (!channels2.changed && channels2.color)
      return channels2.color;
    if (channels2.type.is(TYPE.HSL) || channels2.data.r === void 0) {
      return hsl_default.stringify(channels2);
    } else if (channels2.a < 1 || !Number.isInteger(channels2.r) || !Number.isInteger(channels2.g) || !Number.isInteger(channels2.b)) {
      return rgb_default.stringify(channels2);
    } else {
      return hex_default.stringify(channels2);
    }
  }
};
var color_default = Color;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/change.js
var change = (color, channels2) => {
  const ch = color_default.parse(color);
  for (const c in channels2) {
    ch[c] = utils_default.channel.clamp[c](channels2[c]);
  }
  return color_default.stringify(ch);
};
var change_default = change;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/rgba.js
var rgba = (r, g, b = 0, a = 1) => {
  if (typeof r !== "number")
    return change_default(r, { a: g });
  const channels2 = reusable_default.set({
    r: utils_default.channel.clamp.r(r),
    g: utils_default.channel.clamp.g(g),
    b: utils_default.channel.clamp.b(b),
    a: utils_default.channel.clamp.a(a)
  });
  return color_default.stringify(channels2);
};
var rgba_default = rgba;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/channel.js
var channel = (color, channel2) => {
  return utils_default.lang.round(color_default.parse(color)[channel2]);
};
var channel_default2 = channel;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/luminance.js
var luminance = (color) => {
  const { r, g, b } = color_default.parse(color);
  const luminance2 = 0.2126 * utils_default.channel.toLinear(r) + 0.7152 * utils_default.channel.toLinear(g) + 0.0722 * utils_default.channel.toLinear(b);
  return utils_default.lang.round(luminance2);
};
var luminance_default = luminance;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/is_light.js
var isLight = (color) => {
  return luminance_default(color) >= 0.5;
};
var is_light_default = isLight;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/is_dark.js
var isDark = (color) => {
  return !is_light_default(color);
};
var is_dark_default = isDark;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/adjust_channel.js
var adjustChannel = (color, channel2, amount) => {
  const channels2 = color_default.parse(color);
  const amountCurrent = channels2[channel2];
  const amountNext = utils_default.channel.clamp[channel2](amountCurrent + amount);
  if (amountCurrent !== amountNext)
    channels2[channel2] = amountNext;
  return color_default.stringify(channels2);
};
var adjust_channel_default = adjustChannel;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/lighten.js
var lighten = (color, amount) => {
  return adjust_channel_default(color, "l", amount);
};
var lighten_default = lighten;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/darken.js
var darken = (color, amount) => {
  return adjust_channel_default(color, "l", -amount);
};
var darken_default = darken;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/transparentize.js
var transparentize = (color, amount) => {
  return adjust_channel_default(color, "a", -amount);
};
var transparentize_default = transparentize;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/adjust.js
var adjust = (color, channels2) => {
  const ch = color_default.parse(color);
  const changes = {};
  for (const c in channels2) {
    if (!channels2[c])
      continue;
    changes[c] = ch[c] + channels2[c];
  }
  return change_default(color, changes);
};
var adjust_default = adjust;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/mix.js
var mix = (color1, color2, weight = 50) => {
  const { r: r1, g: g1, b: b1, a: a1 } = color_default.parse(color1);
  const { r: r2, g: g2, b: b2, a: a2 } = color_default.parse(color2);
  const weightScale = weight / 100;
  const weightNormalized = weightScale * 2 - 1;
  const alphaDelta = a1 - a2;
  const weight1combined = weightNormalized * alphaDelta === -1 ? weightNormalized : (weightNormalized + alphaDelta) / (1 + weightNormalized * alphaDelta);
  const weight1 = (weight1combined + 1) / 2;
  const weight2 = 1 - weight1;
  const r = r1 * weight1 + r2 * weight2;
  const g = g1 * weight1 + g2 * weight2;
  const b = b1 * weight1 + b2 * weight2;
  const a = a1 * weightScale + a2 * (1 - weightScale);
  return rgba_default(r, g, b, a);
};
var mix_default = mix;

// node_modules/.pnpm/khroma@2.1.0/node_modules/khroma/dist/methods/invert.js
var invert = (color, weight = 100) => {
  const inverse = color_default.parse(color);
  inverse.r = 255 - inverse.r;
  inverse.g = 255 - inverse.g;
  inverse.b = 255 - inverse.b;
  return mix_default(inverse, color, weight);
};
var invert_default = invert;

// node_modules/.pnpm/mermaid@11.13.0/node_modules/mermaid/dist/chunks/mermaid.core/chunk-7R4GIKGN.mjs
var frontMatterRegex = /^-{3}\s*[\n\r](.*?)[\n\r]-{3}\s*[\n\r]+/s;
var directiveRegex = /%{2}{\s*(?:(\w+)\s*:|(\w+))\s*(?:(\w+)|((?:(?!}%{2}).|\r?\n)*))?\s*(?:}%{2})?/gi;
var anyCommentRegex = /\s*%%.*\n/gm;
var _a;
var UnknownDiagramError = (_a = class extends Error {
  constructor(message) {
    super(message);
    this.name = "UnknownDiagramError";
  }
}, __name(_a, "UnknownDiagramError"), _a);
var detectors = {};
var detectType = /* @__PURE__ */ __name(function(text, config2) {
  text = text.replace(frontMatterRegex, "").replace(directiveRegex, "").replace(anyCommentRegex, "\n");
  for (const [key, { detector }] of Object.entries(detectors)) {
    const diagram = detector(text, config2);
    if (diagram) {
      return key;
    }
  }
  throw new UnknownDiagramError(
    `No diagram type detected matching given configuration for text: ${text}`
  );
}, "detectType");
var registerLazyLoadedDiagrams = /* @__PURE__ */ __name((...diagrams2) => {
  for (const { id, detector, loader } of diagrams2) {
    addDetector(id, detector, loader);
  }
}, "registerLazyLoadedDiagrams");
var addDetector = /* @__PURE__ */ __name((key, detector, loader) => {
  if (detectors[key]) {
    log.warn(`Detector with key ${key} already exists. Overwriting.`);
  }
  detectors[key] = { detector, loader };
  log.debug(`Detector with key ${key} added${loader ? " with loader" : ""}`);
}, "addDetector");
var getDiagramLoader = /* @__PURE__ */ __name((key) => {
  return detectors[key].loader;
}, "getDiagramLoader");
var assignWithDepth = /* @__PURE__ */ __name((dst, src, { depth = 2, clobber = false } = {}) => {
  const config2 = { depth, clobber };
  if (Array.isArray(src) && !Array.isArray(dst)) {
    src.forEach((s) => assignWithDepth(dst, s, config2));
    return dst;
  } else if (Array.isArray(src) && Array.isArray(dst)) {
    src.forEach((s) => {
      if (!dst.includes(s)) {
        dst.push(s);
      }
    });
    return dst;
  }
  if (dst === void 0 || depth <= 0) {
    if (dst !== void 0 && dst !== null && typeof dst === "object" && typeof src === "object") {
      return Object.assign(dst, src);
    } else {
      return src;
    }
  }
  if (src !== void 0 && typeof dst === "object" && typeof src === "object") {
    Object.keys(src).forEach((key) => {
      if (typeof src[key] === "object" && src[key] !== null && (dst[key] === void 0 || typeof dst[key] === "object")) {
        if (dst[key] === void 0) {
          dst[key] = Array.isArray(src[key]) ? [] : {};
        }
        dst[key] = assignWithDepth(dst[key], src[key], { depth: depth - 1, clobber });
      } else if (clobber || typeof dst[key] !== "object" && typeof src[key] !== "object") {
        dst[key] = src[key];
      }
    });
  }
  return dst;
}, "assignWithDepth");
var assignWithDepth_default = assignWithDepth;
var oldAttributeBackgroundColorOdd = "#ffffff";
var oldAttributeBackgroundColorEven = "#f2f2f2";
var mkBorder = /* @__PURE__ */ __name((col, darkMode) => darkMode ? adjust_default(col, { s: -40, l: 10 }) : adjust_default(col, { s: -40, l: -10 }), "mkBorder");
var _a2;
var Theme = (_a2 = class {
  constructor() {
    this.background = "#f4f4f4";
    this.primaryColor = "#fff4dd";
    this.noteBkgColor = "#fff5ad";
    this.noteTextColor = "#333";
    this.THEME_COLOR_LIMIT = 12;
    this.fontFamily = '"trebuchet ms", verdana, arial, sans-serif';
    this.fontSize = "16px";
  }
  updateColors() {
    this.primaryTextColor = this.primaryTextColor || (this.darkMode ? "#eee" : "#333");
    this.secondaryColor = this.secondaryColor || adjust_default(this.primaryColor, { h: -120 });
    this.tertiaryColor = this.tertiaryColor || adjust_default(this.primaryColor, { h: 180, l: 5 });
    this.primaryBorderColor = this.primaryBorderColor || mkBorder(this.primaryColor, this.darkMode);
    this.secondaryBorderColor = this.secondaryBorderColor || mkBorder(this.secondaryColor, this.darkMode);
    this.tertiaryBorderColor = this.tertiaryBorderColor || mkBorder(this.tertiaryColor, this.darkMode);
    this.noteBorderColor = this.noteBorderColor || mkBorder(this.noteBkgColor, this.darkMode);
    this.noteBkgColor = this.noteBkgColor || "#fff5ad";
    this.noteTextColor = this.noteTextColor || "#333";
    this.secondaryTextColor = this.secondaryTextColor || invert_default(this.secondaryColor);
    this.tertiaryTextColor = this.tertiaryTextColor || invert_default(this.tertiaryColor);
    this.lineColor = this.lineColor || invert_default(this.background);
    this.arrowheadColor = this.arrowheadColor || invert_default(this.background);
    this.textColor = this.textColor || this.primaryTextColor;
    this.border2 = this.border2 || this.tertiaryBorderColor;
    this.nodeBkg = this.nodeBkg || this.primaryColor;
    this.mainBkg = this.mainBkg || this.primaryColor;
    this.nodeBorder = this.nodeBorder || this.primaryBorderColor;
    this.clusterBkg = this.clusterBkg || this.tertiaryColor;
    this.clusterBorder = this.clusterBorder || this.tertiaryBorderColor;
    this.defaultLinkColor = this.defaultLinkColor || this.lineColor;
    this.titleColor = this.titleColor || this.tertiaryTextColor;
    this.edgeLabelBackground = this.edgeLabelBackground || (this.darkMode ? darken_default(this.secondaryColor, 30) : this.secondaryColor);
    this.nodeTextColor = this.nodeTextColor || this.primaryTextColor;
    this.actorBorder = this.actorBorder || this.primaryBorderColor;
    this.actorBkg = this.actorBkg || this.mainBkg;
    this.actorTextColor = this.actorTextColor || this.primaryTextColor;
    this.actorLineColor = this.actorLineColor || this.actorBorder;
    this.labelBoxBkgColor = this.labelBoxBkgColor || this.actorBkg;
    this.signalColor = this.signalColor || this.textColor;
    this.signalTextColor = this.signalTextColor || this.textColor;
    this.labelBoxBorderColor = this.labelBoxBorderColor || this.actorBorder;
    this.labelTextColor = this.labelTextColor || this.actorTextColor;
    this.loopTextColor = this.loopTextColor || this.actorTextColor;
    this.activationBorderColor = this.activationBorderColor || darken_default(this.secondaryColor, 10);
    this.activationBkgColor = this.activationBkgColor || this.secondaryColor;
    this.sequenceNumberColor = this.sequenceNumberColor || invert_default(this.lineColor);
    this.sectionBkgColor = this.sectionBkgColor || this.tertiaryColor;
    this.altSectionBkgColor = this.altSectionBkgColor || "white";
    this.sectionBkgColor = this.sectionBkgColor || this.secondaryColor;
    this.sectionBkgColor2 = this.sectionBkgColor2 || this.primaryColor;
    this.excludeBkgColor = this.excludeBkgColor || "#eeeeee";
    this.taskBorderColor = this.taskBorderColor || this.primaryBorderColor;
    this.taskBkgColor = this.taskBkgColor || this.primaryColor;
    this.activeTaskBorderColor = this.activeTaskBorderColor || this.primaryColor;
    this.activeTaskBkgColor = this.activeTaskBkgColor || lighten_default(this.primaryColor, 23);
    this.gridColor = this.gridColor || "lightgrey";
    this.doneTaskBkgColor = this.doneTaskBkgColor || "lightgrey";
    this.doneTaskBorderColor = this.doneTaskBorderColor || "grey";
    this.critBorderColor = this.critBorderColor || "#ff8888";
    this.critBkgColor = this.critBkgColor || "red";
    this.todayLineColor = this.todayLineColor || "red";
    this.vertLineColor = this.vertLineColor || "navy";
    this.taskTextColor = this.taskTextColor || this.textColor;
    this.taskTextOutsideColor = this.taskTextOutsideColor || this.textColor;
    this.taskTextLightColor = this.taskTextLightColor || this.textColor;
    this.taskTextColor = this.taskTextColor || this.primaryTextColor;
    this.taskTextDarkColor = this.taskTextDarkColor || this.textColor;
    this.taskTextClickableColor = this.taskTextClickableColor || "#003163";
    this.personBorder = this.personBorder || this.primaryBorderColor;
    this.personBkg = this.personBkg || this.mainBkg;
    if (this.darkMode) {
      this.rowOdd = this.rowOdd || darken_default(this.mainBkg, 5) || "#ffffff";
      this.rowEven = this.rowEven || darken_default(this.mainBkg, 10);
    } else {
      this.rowOdd = this.rowOdd || lighten_default(this.mainBkg, 75) || "#ffffff";
      this.rowEven = this.rowEven || lighten_default(this.mainBkg, 5);
    }
    this.transitionColor = this.transitionColor || this.lineColor;
    this.transitionLabelColor = this.transitionLabelColor || this.textColor;
    this.stateLabelColor = this.stateLabelColor || this.stateBkg || this.primaryTextColor;
    this.stateBkg = this.stateBkg || this.mainBkg;
    this.labelBackgroundColor = this.labelBackgroundColor || this.stateBkg;
    this.compositeBackground = this.compositeBackground || this.background || this.tertiaryColor;
    this.altBackground = this.altBackground || this.tertiaryColor;
    this.compositeTitleBackground = this.compositeTitleBackground || this.mainBkg;
    this.compositeBorder = this.compositeBorder || this.nodeBorder;
    this.innerEndBackground = this.nodeBorder;
    this.errorBkgColor = this.errorBkgColor || this.tertiaryColor;
    this.errorTextColor = this.errorTextColor || this.tertiaryTextColor;
    this.transitionColor = this.transitionColor || this.lineColor;
    this.specialStateColor = this.lineColor;
    this.cScale0 = this.cScale0 || this.primaryColor;
    this.cScale1 = this.cScale1 || this.secondaryColor;
    this.cScale2 = this.cScale2 || this.tertiaryColor;
    this.cScale3 = this.cScale3 || adjust_default(this.primaryColor, { h: 30 });
    this.cScale4 = this.cScale4 || adjust_default(this.primaryColor, { h: 60 });
    this.cScale5 = this.cScale5 || adjust_default(this.primaryColor, { h: 90 });
    this.cScale6 = this.cScale6 || adjust_default(this.primaryColor, { h: 120 });
    this.cScale7 = this.cScale7 || adjust_default(this.primaryColor, { h: 150 });
    this.cScale8 = this.cScale8 || adjust_default(this.primaryColor, { h: 210, l: 150 });
    this.cScale9 = this.cScale9 || adjust_default(this.primaryColor, { h: 270 });
    this.cScale10 = this.cScale10 || adjust_default(this.primaryColor, { h: 300 });
    this.cScale11 = this.cScale11 || adjust_default(this.primaryColor, { h: 330 });
    if (this.darkMode) {
      for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
        this["cScale" + i] = darken_default(this["cScale" + i], 75);
      }
    } else {
      for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
        this["cScale" + i] = darken_default(this["cScale" + i], 25);
      }
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleInv" + i] = this["cScaleInv" + i] || invert_default(this["cScale" + i]);
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      if (this.darkMode) {
        this["cScalePeer" + i] = this["cScalePeer" + i] || lighten_default(this["cScale" + i], 10);
      } else {
        this["cScalePeer" + i] = this["cScalePeer" + i] || darken_default(this["cScale" + i], 10);
      }
    }
    this.scaleLabelColor = this.scaleLabelColor || this.labelTextColor;
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleLabel" + i] = this["cScaleLabel" + i] || this.scaleLabelColor;
    }
    const multiplier = this.darkMode ? -4 : -1;
    for (let i = 0; i < 5; i++) {
      this["surface" + i] = this["surface" + i] || adjust_default(this.mainBkg, { h: 180, s: -15, l: multiplier * (5 + i * 3) });
      this["surfacePeer" + i] = this["surfacePeer" + i] || adjust_default(this.mainBkg, { h: 180, s: -15, l: multiplier * (8 + i * 3) });
    }
    this.classText = this.classText || this.textColor;
    this.fillType0 = this.fillType0 || this.primaryColor;
    this.fillType1 = this.fillType1 || this.secondaryColor;
    this.fillType2 = this.fillType2 || adjust_default(this.primaryColor, { h: 64 });
    this.fillType3 = this.fillType3 || adjust_default(this.secondaryColor, { h: 64 });
    this.fillType4 = this.fillType4 || adjust_default(this.primaryColor, { h: -64 });
    this.fillType5 = this.fillType5 || adjust_default(this.secondaryColor, { h: -64 });
    this.fillType6 = this.fillType6 || adjust_default(this.primaryColor, { h: 128 });
    this.fillType7 = this.fillType7 || adjust_default(this.secondaryColor, { h: 128 });
    this.pie1 = this.pie1 || this.primaryColor;
    this.pie2 = this.pie2 || this.secondaryColor;
    this.pie3 = this.pie3 || this.tertiaryColor;
    this.pie4 = this.pie4 || adjust_default(this.primaryColor, { l: -10 });
    this.pie5 = this.pie5 || adjust_default(this.secondaryColor, { l: -10 });
    this.pie6 = this.pie6 || adjust_default(this.tertiaryColor, { l: -10 });
    this.pie7 = this.pie7 || adjust_default(this.primaryColor, { h: 60, l: -10 });
    this.pie8 = this.pie8 || adjust_default(this.primaryColor, { h: -60, l: -10 });
    this.pie9 = this.pie9 || adjust_default(this.primaryColor, { h: 120, l: 0 });
    this.pie10 = this.pie10 || adjust_default(this.primaryColor, { h: 60, l: -20 });
    this.pie11 = this.pie11 || adjust_default(this.primaryColor, { h: -60, l: -20 });
    this.pie12 = this.pie12 || adjust_default(this.primaryColor, { h: 120, l: -10 });
    this.pieTitleTextSize = this.pieTitleTextSize || "25px";
    this.pieTitleTextColor = this.pieTitleTextColor || this.taskTextDarkColor;
    this.pieSectionTextSize = this.pieSectionTextSize || "17px";
    this.pieSectionTextColor = this.pieSectionTextColor || this.textColor;
    this.pieLegendTextSize = this.pieLegendTextSize || "17px";
    this.pieLegendTextColor = this.pieLegendTextColor || this.taskTextDarkColor;
    this.pieStrokeColor = this.pieStrokeColor || "black";
    this.pieStrokeWidth = this.pieStrokeWidth || "2px";
    this.pieOuterStrokeWidth = this.pieOuterStrokeWidth || "2px";
    this.pieOuterStrokeColor = this.pieOuterStrokeColor || "black";
    this.pieOpacity = this.pieOpacity || "0.7";
    this.venn1 = this.venn1 ?? adjust_default(this.primaryColor, { l: -30 });
    this.venn2 = this.venn2 ?? adjust_default(this.secondaryColor, { l: -30 });
    this.venn3 = this.venn3 ?? adjust_default(this.tertiaryColor, { l: -30 });
    this.venn4 = this.venn4 ?? adjust_default(this.primaryColor, { h: 60, l: -30 });
    this.venn5 = this.venn5 ?? adjust_default(this.primaryColor, { h: -60, l: -30 });
    this.venn6 = this.venn6 ?? adjust_default(this.secondaryColor, { h: 60, l: -30 });
    this.venn7 = this.venn7 ?? adjust_default(this.primaryColor, { h: 120, l: -30 });
    this.venn8 = this.venn8 ?? adjust_default(this.secondaryColor, { h: 120, l: -30 });
    this.vennTitleTextColor = this.vennTitleTextColor ?? this.titleColor;
    this.vennSetTextColor = this.vennSetTextColor ?? this.textColor;
    this.radar = {
      axisColor: this.radar?.axisColor || this.lineColor,
      axisStrokeWidth: this.radar?.axisStrokeWidth || 2,
      axisLabelFontSize: this.radar?.axisLabelFontSize || 12,
      curveOpacity: this.radar?.curveOpacity || 0.5,
      curveStrokeWidth: this.radar?.curveStrokeWidth || 2,
      graticuleColor: this.radar?.graticuleColor || "#DEDEDE",
      graticuleStrokeWidth: this.radar?.graticuleStrokeWidth || 1,
      graticuleOpacity: this.radar?.graticuleOpacity || 0.3,
      legendBoxSize: this.radar?.legendBoxSize || 12,
      legendFontSize: this.radar?.legendFontSize || 12
    };
    this.archEdgeColor = this.archEdgeColor || "#777";
    this.archEdgeArrowColor = this.archEdgeArrowColor || "#777";
    this.archEdgeWidth = this.archEdgeWidth || "3";
    this.archGroupBorderColor = this.archGroupBorderColor || "#000";
    this.archGroupBorderWidth = this.archGroupBorderWidth || "2px";
    this.quadrant1Fill = this.quadrant1Fill || this.primaryColor;
    this.quadrant2Fill = this.quadrant2Fill || adjust_default(this.primaryColor, { r: 5, g: 5, b: 5 });
    this.quadrant3Fill = this.quadrant3Fill || adjust_default(this.primaryColor, { r: 10, g: 10, b: 10 });
    this.quadrant4Fill = this.quadrant4Fill || adjust_default(this.primaryColor, { r: 15, g: 15, b: 15 });
    this.quadrant1TextFill = this.quadrant1TextFill || this.primaryTextColor;
    this.quadrant2TextFill = this.quadrant2TextFill || adjust_default(this.primaryTextColor, { r: -5, g: -5, b: -5 });
    this.quadrant3TextFill = this.quadrant3TextFill || adjust_default(this.primaryTextColor, { r: -10, g: -10, b: -10 });
    this.quadrant4TextFill = this.quadrant4TextFill || adjust_default(this.primaryTextColor, { r: -15, g: -15, b: -15 });
    this.quadrantPointFill = this.quadrantPointFill || is_dark_default(this.quadrant1Fill) ? lighten_default(this.quadrant1Fill) : darken_default(this.quadrant1Fill);
    this.quadrantPointTextFill = this.quadrantPointTextFill || this.primaryTextColor;
    this.quadrantXAxisTextFill = this.quadrantXAxisTextFill || this.primaryTextColor;
    this.quadrantYAxisTextFill = this.quadrantYAxisTextFill || this.primaryTextColor;
    this.quadrantInternalBorderStrokeFill = this.quadrantInternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantExternalBorderStrokeFill = this.quadrantExternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantTitleFill = this.quadrantTitleFill || this.primaryTextColor;
    this.xyChart = {
      backgroundColor: this.xyChart?.backgroundColor || this.background,
      titleColor: this.xyChart?.titleColor || this.primaryTextColor,
      xAxisTitleColor: this.xyChart?.xAxisTitleColor || this.primaryTextColor,
      xAxisLabelColor: this.xyChart?.xAxisLabelColor || this.primaryTextColor,
      xAxisTickColor: this.xyChart?.xAxisTickColor || this.primaryTextColor,
      xAxisLineColor: this.xyChart?.xAxisLineColor || this.primaryTextColor,
      yAxisTitleColor: this.xyChart?.yAxisTitleColor || this.primaryTextColor,
      yAxisLabelColor: this.xyChart?.yAxisLabelColor || this.primaryTextColor,
      yAxisTickColor: this.xyChart?.yAxisTickColor || this.primaryTextColor,
      yAxisLineColor: this.xyChart?.yAxisLineColor || this.primaryTextColor,
      plotColorPalette: this.xyChart?.plotColorPalette || "#FFF4DD,#FFD8B1,#FFA07A,#ECEFF1,#D6DBDF,#C3E0A8,#FFB6A4,#FFD74D,#738FA7,#FFFFF0"
    };
    this.requirementBackground = this.requirementBackground || this.primaryColor;
    this.requirementBorderColor = this.requirementBorderColor || this.primaryBorderColor;
    this.requirementBorderSize = this.requirementBorderSize || "1";
    this.requirementTextColor = this.requirementTextColor || this.primaryTextColor;
    this.relationColor = this.relationColor || this.lineColor;
    this.relationLabelBackground = this.relationLabelBackground || (this.darkMode ? darken_default(this.secondaryColor, 30) : this.secondaryColor);
    this.relationLabelColor = this.relationLabelColor || this.actorTextColor;
    this.git0 = this.git0 || this.primaryColor;
    this.git1 = this.git1 || this.secondaryColor;
    this.git2 = this.git2 || this.tertiaryColor;
    this.git3 = this.git3 || adjust_default(this.primaryColor, { h: -30 });
    this.git4 = this.git4 || adjust_default(this.primaryColor, { h: -60 });
    this.git5 = this.git5 || adjust_default(this.primaryColor, { h: -90 });
    this.git6 = this.git6 || adjust_default(this.primaryColor, { h: 60 });
    this.git7 = this.git7 || adjust_default(this.primaryColor, { h: 120 });
    if (this.darkMode) {
      this.git0 = lighten_default(this.git0, 25);
      this.git1 = lighten_default(this.git1, 25);
      this.git2 = lighten_default(this.git2, 25);
      this.git3 = lighten_default(this.git3, 25);
      this.git4 = lighten_default(this.git4, 25);
      this.git5 = lighten_default(this.git5, 25);
      this.git6 = lighten_default(this.git6, 25);
      this.git7 = lighten_default(this.git7, 25);
    } else {
      this.git0 = darken_default(this.git0, 25);
      this.git1 = darken_default(this.git1, 25);
      this.git2 = darken_default(this.git2, 25);
      this.git3 = darken_default(this.git3, 25);
      this.git4 = darken_default(this.git4, 25);
      this.git5 = darken_default(this.git5, 25);
      this.git6 = darken_default(this.git6, 25);
      this.git7 = darken_default(this.git7, 25);
    }
    this.gitInv0 = this.gitInv0 || invert_default(this.git0);
    this.gitInv1 = this.gitInv1 || invert_default(this.git1);
    this.gitInv2 = this.gitInv2 || invert_default(this.git2);
    this.gitInv3 = this.gitInv3 || invert_default(this.git3);
    this.gitInv4 = this.gitInv4 || invert_default(this.git4);
    this.gitInv5 = this.gitInv5 || invert_default(this.git5);
    this.gitInv6 = this.gitInv6 || invert_default(this.git6);
    this.gitInv7 = this.gitInv7 || invert_default(this.git7);
    this.branchLabelColor = this.branchLabelColor || (this.darkMode ? "black" : this.labelTextColor);
    this.gitBranchLabel0 = this.gitBranchLabel0 || this.branchLabelColor;
    this.gitBranchLabel1 = this.gitBranchLabel1 || this.branchLabelColor;
    this.gitBranchLabel2 = this.gitBranchLabel2 || this.branchLabelColor;
    this.gitBranchLabel3 = this.gitBranchLabel3 || this.branchLabelColor;
    this.gitBranchLabel4 = this.gitBranchLabel4 || this.branchLabelColor;
    this.gitBranchLabel5 = this.gitBranchLabel5 || this.branchLabelColor;
    this.gitBranchLabel6 = this.gitBranchLabel6 || this.branchLabelColor;
    this.gitBranchLabel7 = this.gitBranchLabel7 || this.branchLabelColor;
    this.tagLabelColor = this.tagLabelColor || this.primaryTextColor;
    this.tagLabelBackground = this.tagLabelBackground || this.primaryColor;
    this.tagLabelBorder = this.tagBorder || this.primaryBorderColor;
    this.tagLabelFontSize = this.tagLabelFontSize || "10px";
    this.commitLabelColor = this.commitLabelColor || this.secondaryTextColor;
    this.commitLabelBackground = this.commitLabelBackground || this.secondaryColor;
    this.commitLabelFontSize = this.commitLabelFontSize || "10px";
    this.attributeBackgroundColorOdd = this.attributeBackgroundColorOdd || oldAttributeBackgroundColorOdd;
    this.attributeBackgroundColorEven = this.attributeBackgroundColorEven || oldAttributeBackgroundColorEven;
  }
  calculate(overrides) {
    if (typeof overrides !== "object") {
      this.updateColors();
      return;
    }
    const keys = Object.keys(overrides);
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
    this.updateColors();
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
  }
}, __name(_a2, "Theme"), _a2);
var getThemeVariables = /* @__PURE__ */ __name((userOverrides) => {
  const theme = new Theme();
  theme.calculate(userOverrides);
  return theme;
}, "getThemeVariables");
var _a3;
var Theme2 = (_a3 = class {
  constructor() {
    this.background = "#333";
    this.primaryColor = "#1f2020";
    this.secondaryColor = lighten_default(this.primaryColor, 16);
    this.tertiaryColor = adjust_default(this.primaryColor, { h: -160 });
    this.primaryBorderColor = invert_default(this.background);
    this.secondaryBorderColor = mkBorder(this.secondaryColor, this.darkMode);
    this.tertiaryBorderColor = mkBorder(this.tertiaryColor, this.darkMode);
    this.primaryTextColor = invert_default(this.primaryColor);
    this.secondaryTextColor = invert_default(this.secondaryColor);
    this.tertiaryTextColor = invert_default(this.tertiaryColor);
    this.lineColor = invert_default(this.background);
    this.textColor = invert_default(this.background);
    this.mainBkg = "#1f2020";
    this.secondBkg = "calculated";
    this.mainContrastColor = "lightgrey";
    this.darkTextColor = lighten_default(invert_default("#323D47"), 10);
    this.lineColor = "calculated";
    this.border1 = "#ccc";
    this.border2 = rgba_default(255, 255, 255, 0.25);
    this.arrowheadColor = "calculated";
    this.fontFamily = '"trebuchet ms", verdana, arial, sans-serif';
    this.fontSize = "16px";
    this.labelBackground = "#181818";
    this.textColor = "#ccc";
    this.THEME_COLOR_LIMIT = 12;
    this.nodeBkg = "calculated";
    this.nodeBorder = "calculated";
    this.clusterBkg = "calculated";
    this.clusterBorder = "calculated";
    this.defaultLinkColor = "calculated";
    this.titleColor = "#F9FFFE";
    this.edgeLabelBackground = "calculated";
    this.actorBorder = "calculated";
    this.actorBkg = "calculated";
    this.actorTextColor = "calculated";
    this.actorLineColor = "calculated";
    this.signalColor = "calculated";
    this.signalTextColor = "calculated";
    this.labelBoxBkgColor = "calculated";
    this.labelBoxBorderColor = "calculated";
    this.labelTextColor = "calculated";
    this.loopTextColor = "calculated";
    this.noteBorderColor = "calculated";
    this.noteBkgColor = "#fff5ad";
    this.noteTextColor = "calculated";
    this.activationBorderColor = "calculated";
    this.activationBkgColor = "calculated";
    this.sequenceNumberColor = "black";
    this.sectionBkgColor = darken_default("#EAE8D9", 30);
    this.altSectionBkgColor = "calculated";
    this.sectionBkgColor2 = "#EAE8D9";
    this.excludeBkgColor = darken_default(this.sectionBkgColor, 10);
    this.taskBorderColor = rgba_default(255, 255, 255, 70);
    this.taskBkgColor = "calculated";
    this.taskTextColor = "calculated";
    this.taskTextLightColor = "calculated";
    this.taskTextOutsideColor = "calculated";
    this.taskTextClickableColor = "#003163";
    this.activeTaskBorderColor = rgba_default(255, 255, 255, 50);
    this.activeTaskBkgColor = "#81B1DB";
    this.gridColor = "calculated";
    this.doneTaskBkgColor = "calculated";
    this.doneTaskBorderColor = "grey";
    this.critBorderColor = "#E83737";
    this.critBkgColor = "#E83737";
    this.taskTextDarkColor = "calculated";
    this.todayLineColor = "#DB5757";
    this.vertLineColor = "#00BFFF";
    this.personBorder = this.primaryBorderColor;
    this.personBkg = this.mainBkg;
    this.archEdgeColor = "calculated";
    this.archEdgeArrowColor = "calculated";
    this.archEdgeWidth = "3";
    this.archGroupBorderColor = this.primaryBorderColor;
    this.archGroupBorderWidth = "2px";
    this.rowOdd = this.rowOdd || lighten_default(this.mainBkg, 5) || "#ffffff";
    this.rowEven = this.rowEven || darken_default(this.mainBkg, 10);
    this.labelColor = "calculated";
    this.errorBkgColor = "#a44141";
    this.errorTextColor = "#ddd";
  }
  updateColors() {
    this.secondBkg = lighten_default(this.mainBkg, 16);
    this.lineColor = this.mainContrastColor;
    this.arrowheadColor = this.mainContrastColor;
    this.nodeBkg = this.mainBkg;
    this.nodeBorder = this.border1;
    this.clusterBkg = this.secondBkg;
    this.clusterBorder = this.border2;
    this.defaultLinkColor = this.lineColor;
    this.edgeLabelBackground = lighten_default(this.labelBackground, 25);
    this.actorBorder = this.border1;
    this.actorBkg = this.mainBkg;
    this.actorTextColor = this.mainContrastColor;
    this.actorLineColor = this.actorBorder;
    this.signalColor = this.mainContrastColor;
    this.signalTextColor = this.mainContrastColor;
    this.labelBoxBkgColor = this.actorBkg;
    this.labelBoxBorderColor = this.actorBorder;
    this.labelTextColor = this.mainContrastColor;
    this.loopTextColor = this.mainContrastColor;
    this.noteBorderColor = this.secondaryBorderColor;
    this.noteBkgColor = this.secondBkg;
    this.noteTextColor = this.secondaryTextColor;
    this.activationBorderColor = this.border1;
    this.activationBkgColor = this.secondBkg;
    this.altSectionBkgColor = this.background;
    this.taskBkgColor = lighten_default(this.mainBkg, 23);
    this.taskTextColor = this.darkTextColor;
    this.taskTextLightColor = this.mainContrastColor;
    this.taskTextOutsideColor = this.taskTextLightColor;
    this.gridColor = this.mainContrastColor;
    this.doneTaskBkgColor = this.mainContrastColor;
    this.taskTextDarkColor = invert_default(this.doneTaskBkgColor);
    this.archEdgeColor = this.lineColor;
    this.archEdgeArrowColor = this.lineColor;
    this.transitionColor = this.transitionColor || this.lineColor;
    this.transitionLabelColor = this.transitionLabelColor || this.textColor;
    this.stateLabelColor = this.stateLabelColor || this.stateBkg || this.primaryTextColor;
    this.stateBkg = this.stateBkg || this.mainBkg;
    this.labelBackgroundColor = this.labelBackgroundColor || this.stateBkg;
    this.compositeBackground = this.compositeBackground || this.background || this.tertiaryColor;
    this.altBackground = this.altBackground || "#555";
    this.compositeTitleBackground = this.compositeTitleBackground || this.mainBkg;
    this.compositeBorder = this.compositeBorder || this.nodeBorder;
    this.innerEndBackground = this.primaryBorderColor;
    this.specialStateColor = "#f4f4f4";
    this.errorBkgColor = this.errorBkgColor || this.tertiaryColor;
    this.errorTextColor = this.errorTextColor || this.tertiaryTextColor;
    this.fillType0 = this.primaryColor;
    this.fillType1 = this.secondaryColor;
    this.fillType2 = adjust_default(this.primaryColor, { h: 64 });
    this.fillType3 = adjust_default(this.secondaryColor, { h: 64 });
    this.fillType4 = adjust_default(this.primaryColor, { h: -64 });
    this.fillType5 = adjust_default(this.secondaryColor, { h: -64 });
    this.fillType6 = adjust_default(this.primaryColor, { h: 128 });
    this.fillType7 = adjust_default(this.secondaryColor, { h: 128 });
    this.cScale1 = this.cScale1 || "#0b0000";
    this.cScale2 = this.cScale2 || "#4d1037";
    this.cScale3 = this.cScale3 || "#3f5258";
    this.cScale4 = this.cScale4 || "#4f2f1b";
    this.cScale5 = this.cScale5 || "#6e0a0a";
    this.cScale6 = this.cScale6 || "#3b0048";
    this.cScale7 = this.cScale7 || "#995a01";
    this.cScale8 = this.cScale8 || "#154706";
    this.cScale9 = this.cScale9 || "#161722";
    this.cScale10 = this.cScale10 || "#00296f";
    this.cScale11 = this.cScale11 || "#01629c";
    this.cScale12 = this.cScale12 || "#010029";
    this.cScale0 = this.cScale0 || this.primaryColor;
    this.cScale1 = this.cScale1 || this.secondaryColor;
    this.cScale2 = this.cScale2 || this.tertiaryColor;
    this.cScale3 = this.cScale3 || adjust_default(this.primaryColor, { h: 30 });
    this.cScale4 = this.cScale4 || adjust_default(this.primaryColor, { h: 60 });
    this.cScale5 = this.cScale5 || adjust_default(this.primaryColor, { h: 90 });
    this.cScale6 = this.cScale6 || adjust_default(this.primaryColor, { h: 120 });
    this.cScale7 = this.cScale7 || adjust_default(this.primaryColor, { h: 150 });
    this.cScale8 = this.cScale8 || adjust_default(this.primaryColor, { h: 210 });
    this.cScale9 = this.cScale9 || adjust_default(this.primaryColor, { h: 270 });
    this.cScale10 = this.cScale10 || adjust_default(this.primaryColor, { h: 300 });
    this.cScale11 = this.cScale11 || adjust_default(this.primaryColor, { h: 330 });
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleInv" + i] = this["cScaleInv" + i] || invert_default(this["cScale" + i]);
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScalePeer" + i] = this["cScalePeer" + i] || lighten_default(this["cScale" + i], 10);
    }
    for (let i = 0; i < 5; i++) {
      this["surface" + i] = this["surface" + i] || adjust_default(this.mainBkg, { h: 30, s: -30, l: -(-10 + i * 4) });
      this["surfacePeer" + i] = this["surfacePeer" + i] || adjust_default(this.mainBkg, { h: 30, s: -30, l: -(-7 + i * 4) });
    }
    this.scaleLabelColor = this.scaleLabelColor || (this.darkMode ? "black" : this.labelTextColor);
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleLabel" + i] = this["cScaleLabel" + i] || this.scaleLabelColor;
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["pie" + i] = this["cScale" + i];
    }
    this.pieTitleTextSize = this.pieTitleTextSize || "25px";
    this.pieTitleTextColor = this.pieTitleTextColor || this.mainContrastColor;
    this.pieSectionTextSize = this.pieSectionTextSize || "17px";
    this.pieSectionTextColor = this.pieSectionTextColor || this.textColor;
    this.pieLegendTextSize = this.pieLegendTextSize || "17px";
    this.pieLegendTextColor = this.pieLegendTextColor || this.mainContrastColor;
    this.pieStrokeColor = this.pieStrokeColor || "black";
    this.pieStrokeWidth = this.pieStrokeWidth || "2px";
    this.pieOuterStrokeWidth = this.pieOuterStrokeWidth || "2px";
    this.pieOuterStrokeColor = this.pieOuterStrokeColor || "black";
    this.pieOpacity = this.pieOpacity || "0.7";
    for (let i = 0; i < 8; i++) {
      this["venn" + (i + 1)] = this["venn" + (i + 1)] ?? lighten_default(this["cScale" + i], 30);
    }
    this.vennTitleTextColor = this.vennTitleTextColor ?? this.titleColor;
    this.vennSetTextColor = this.vennSetTextColor ?? this.textColor;
    this.quadrant1Fill = this.quadrant1Fill || this.primaryColor;
    this.quadrant2Fill = this.quadrant2Fill || adjust_default(this.primaryColor, { r: 5, g: 5, b: 5 });
    this.quadrant3Fill = this.quadrant3Fill || adjust_default(this.primaryColor, { r: 10, g: 10, b: 10 });
    this.quadrant4Fill = this.quadrant4Fill || adjust_default(this.primaryColor, { r: 15, g: 15, b: 15 });
    this.quadrant1TextFill = this.quadrant1TextFill || this.primaryTextColor;
    this.quadrant2TextFill = this.quadrant2TextFill || adjust_default(this.primaryTextColor, { r: -5, g: -5, b: -5 });
    this.quadrant3TextFill = this.quadrant3TextFill || adjust_default(this.primaryTextColor, { r: -10, g: -10, b: -10 });
    this.quadrant4TextFill = this.quadrant4TextFill || adjust_default(this.primaryTextColor, { r: -15, g: -15, b: -15 });
    this.quadrantPointFill = this.quadrantPointFill || is_dark_default(this.quadrant1Fill) ? lighten_default(this.quadrant1Fill) : darken_default(this.quadrant1Fill);
    this.quadrantPointTextFill = this.quadrantPointTextFill || this.primaryTextColor;
    this.quadrantXAxisTextFill = this.quadrantXAxisTextFill || this.primaryTextColor;
    this.quadrantYAxisTextFill = this.quadrantYAxisTextFill || this.primaryTextColor;
    this.quadrantInternalBorderStrokeFill = this.quadrantInternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantExternalBorderStrokeFill = this.quadrantExternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantTitleFill = this.quadrantTitleFill || this.primaryTextColor;
    this.xyChart = {
      backgroundColor: this.xyChart?.backgroundColor || this.background,
      titleColor: this.xyChart?.titleColor || this.primaryTextColor,
      xAxisTitleColor: this.xyChart?.xAxisTitleColor || this.primaryTextColor,
      xAxisLabelColor: this.xyChart?.xAxisLabelColor || this.primaryTextColor,
      xAxisTickColor: this.xyChart?.xAxisTickColor || this.primaryTextColor,
      xAxisLineColor: this.xyChart?.xAxisLineColor || this.primaryTextColor,
      yAxisTitleColor: this.xyChart?.yAxisTitleColor || this.primaryTextColor,
      yAxisLabelColor: this.xyChart?.yAxisLabelColor || this.primaryTextColor,
      yAxisTickColor: this.xyChart?.yAxisTickColor || this.primaryTextColor,
      yAxisLineColor: this.xyChart?.yAxisLineColor || this.primaryTextColor,
      plotColorPalette: this.xyChart?.plotColorPalette || "#3498db,#2ecc71,#e74c3c,#f1c40f,#bdc3c7,#ffffff,#34495e,#9b59b6,#1abc9c,#e67e22"
    };
    this.packet = {
      startByteColor: this.primaryTextColor,
      endByteColor: this.primaryTextColor,
      labelColor: this.primaryTextColor,
      titleColor: this.primaryTextColor,
      blockStrokeColor: this.primaryTextColor,
      blockFillColor: this.background
    };
    this.radar = {
      axisColor: this.radar?.axisColor || this.lineColor,
      axisStrokeWidth: this.radar?.axisStrokeWidth || 2,
      axisLabelFontSize: this.radar?.axisLabelFontSize || 12,
      curveOpacity: this.radar?.curveOpacity || 0.5,
      curveStrokeWidth: this.radar?.curveStrokeWidth || 2,
      graticuleColor: this.radar?.graticuleColor || "#DEDEDE",
      graticuleStrokeWidth: this.radar?.graticuleStrokeWidth || 1,
      graticuleOpacity: this.radar?.graticuleOpacity || 0.3,
      legendBoxSize: this.radar?.legendBoxSize || 12,
      legendFontSize: this.radar?.legendFontSize || 12
    };
    this.classText = this.primaryTextColor;
    this.requirementBackground = this.requirementBackground || this.primaryColor;
    this.requirementBorderColor = this.requirementBorderColor || this.primaryBorderColor;
    this.requirementBorderSize = this.requirementBorderSize || "1";
    this.requirementTextColor = this.requirementTextColor || this.primaryTextColor;
    this.relationColor = this.relationColor || this.lineColor;
    this.relationLabelBackground = this.relationLabelBackground || (this.darkMode ? darken_default(this.secondaryColor, 30) : this.secondaryColor);
    this.relationLabelColor = this.relationLabelColor || this.actorTextColor;
    this.git0 = lighten_default(this.secondaryColor, 20);
    this.git1 = lighten_default(this.pie2 || this.secondaryColor, 20);
    this.git2 = lighten_default(this.pie3 || this.tertiaryColor, 20);
    this.git3 = lighten_default(this.pie4 || adjust_default(this.primaryColor, { h: -30 }), 20);
    this.git4 = lighten_default(this.pie5 || adjust_default(this.primaryColor, { h: -60 }), 20);
    this.git5 = lighten_default(this.pie6 || adjust_default(this.primaryColor, { h: -90 }), 10);
    this.git6 = lighten_default(this.pie7 || adjust_default(this.primaryColor, { h: 60 }), 10);
    this.git7 = lighten_default(this.pie8 || adjust_default(this.primaryColor, { h: 120 }), 20);
    this.gitInv0 = this.gitInv0 || invert_default(this.git0);
    this.gitInv1 = this.gitInv1 || invert_default(this.git1);
    this.gitInv2 = this.gitInv2 || invert_default(this.git2);
    this.gitInv3 = this.gitInv3 || invert_default(this.git3);
    this.gitInv4 = this.gitInv4 || invert_default(this.git4);
    this.gitInv5 = this.gitInv5 || invert_default(this.git5);
    this.gitInv6 = this.gitInv6 || invert_default(this.git6);
    this.gitInv7 = this.gitInv7 || invert_default(this.git7);
    this.gitBranchLabel0 = this.gitBranchLabel0 || invert_default(this.labelTextColor);
    this.gitBranchLabel1 = this.gitBranchLabel1 || this.labelTextColor;
    this.gitBranchLabel2 = this.gitBranchLabel2 || this.labelTextColor;
    this.gitBranchLabel3 = this.gitBranchLabel3 || invert_default(this.labelTextColor);
    this.gitBranchLabel4 = this.gitBranchLabel4 || this.labelTextColor;
    this.gitBranchLabel5 = this.gitBranchLabel5 || this.labelTextColor;
    this.gitBranchLabel6 = this.gitBranchLabel6 || this.labelTextColor;
    this.gitBranchLabel7 = this.gitBranchLabel7 || this.labelTextColor;
    this.tagLabelColor = this.tagLabelColor || this.primaryTextColor;
    this.tagLabelBackground = this.tagLabelBackground || this.primaryColor;
    this.tagLabelBorder = this.tagBorder || this.primaryBorderColor;
    this.tagLabelFontSize = this.tagLabelFontSize || "10px";
    this.commitLabelColor = this.commitLabelColor || this.secondaryTextColor;
    this.commitLabelBackground = this.commitLabelBackground || this.secondaryColor;
    this.commitLabelFontSize = this.commitLabelFontSize || "10px";
    this.attributeBackgroundColorOdd = this.attributeBackgroundColorOdd || lighten_default(this.background, 12);
    this.attributeBackgroundColorEven = this.attributeBackgroundColorEven || lighten_default(this.background, 2);
    this.nodeBorder = this.nodeBorder || "#999";
  }
  calculate(overrides) {
    if (typeof overrides !== "object") {
      this.updateColors();
      return;
    }
    const keys = Object.keys(overrides);
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
    this.updateColors();
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
  }
}, __name(_a3, "Theme"), _a3);
var getThemeVariables2 = /* @__PURE__ */ __name((userOverrides) => {
  const theme = new Theme2();
  theme.calculate(userOverrides);
  return theme;
}, "getThemeVariables");
var _a4;
var Theme3 = (_a4 = class {
  constructor() {
    this.background = "#f4f4f4";
    this.primaryColor = "#ECECFF";
    this.secondaryColor = adjust_default(this.primaryColor, { h: 120 });
    this.secondaryColor = "#ffffde";
    this.tertiaryColor = adjust_default(this.primaryColor, { h: -160 });
    this.primaryBorderColor = mkBorder(this.primaryColor, this.darkMode);
    this.secondaryBorderColor = mkBorder(this.secondaryColor, this.darkMode);
    this.tertiaryBorderColor = mkBorder(this.tertiaryColor, this.darkMode);
    this.primaryTextColor = invert_default(this.primaryColor);
    this.secondaryTextColor = invert_default(this.secondaryColor);
    this.tertiaryTextColor = invert_default(this.tertiaryColor);
    this.lineColor = invert_default(this.background);
    this.textColor = invert_default(this.background);
    this.background = "white";
    this.mainBkg = "#ECECFF";
    this.secondBkg = "#ffffde";
    this.lineColor = "#333333";
    this.border1 = "#9370DB";
    this.border2 = "#aaaa33";
    this.arrowheadColor = "#333333";
    this.fontFamily = '"trebuchet ms", verdana, arial, sans-serif';
    this.fontSize = "16px";
    this.labelBackground = "rgba(232,232,232, 0.8)";
    this.textColor = "#333";
    this.THEME_COLOR_LIMIT = 12;
    this.nodeBkg = "calculated";
    this.nodeBorder = "calculated";
    this.clusterBkg = "calculated";
    this.clusterBorder = "calculated";
    this.defaultLinkColor = "calculated";
    this.titleColor = "calculated";
    this.edgeLabelBackground = "calculated";
    this.actorBorder = "calculated";
    this.actorBkg = "calculated";
    this.actorTextColor = "black";
    this.actorLineColor = "calculated";
    this.signalColor = "calculated";
    this.signalTextColor = "calculated";
    this.labelBoxBkgColor = "calculated";
    this.labelBoxBorderColor = "calculated";
    this.labelTextColor = "calculated";
    this.loopTextColor = "calculated";
    this.noteBorderColor = "calculated";
    this.noteBkgColor = "#fff5ad";
    this.noteTextColor = "calculated";
    this.activationBorderColor = "#666";
    this.activationBkgColor = "#f4f4f4";
    this.sequenceNumberColor = "white";
    this.sectionBkgColor = "calculated";
    this.altSectionBkgColor = "calculated";
    this.sectionBkgColor2 = "calculated";
    this.excludeBkgColor = "#eeeeee";
    this.taskBorderColor = "calculated";
    this.taskBkgColor = "calculated";
    this.taskTextLightColor = "calculated";
    this.taskTextColor = this.taskTextLightColor;
    this.taskTextDarkColor = "calculated";
    this.taskTextOutsideColor = this.taskTextDarkColor;
    this.taskTextClickableColor = "calculated";
    this.activeTaskBorderColor = "calculated";
    this.activeTaskBkgColor = "calculated";
    this.gridColor = "calculated";
    this.doneTaskBkgColor = "calculated";
    this.doneTaskBorderColor = "calculated";
    this.critBorderColor = "calculated";
    this.critBkgColor = "calculated";
    this.todayLineColor = "calculated";
    this.vertLineColor = "calculated";
    this.sectionBkgColor = rgba_default(102, 102, 255, 0.49);
    this.altSectionBkgColor = "white";
    this.sectionBkgColor2 = "#fff400";
    this.taskBorderColor = "#534fbc";
    this.taskBkgColor = "#8a90dd";
    this.taskTextLightColor = "white";
    this.taskTextColor = "calculated";
    this.taskTextDarkColor = "black";
    this.taskTextOutsideColor = "calculated";
    this.taskTextClickableColor = "#003163";
    this.activeTaskBorderColor = "#534fbc";
    this.activeTaskBkgColor = "#bfc7ff";
    this.gridColor = "lightgrey";
    this.doneTaskBkgColor = "lightgrey";
    this.doneTaskBorderColor = "grey";
    this.critBorderColor = "#ff8888";
    this.critBkgColor = "red";
    this.todayLineColor = "red";
    this.vertLineColor = "navy";
    this.personBorder = this.primaryBorderColor;
    this.personBkg = this.mainBkg;
    this.archEdgeColor = "calculated";
    this.archEdgeArrowColor = "calculated";
    this.archEdgeWidth = "3";
    this.archGroupBorderColor = this.primaryBorderColor;
    this.archGroupBorderWidth = "2px";
    this.rowOdd = "calculated";
    this.rowEven = "calculated";
    this.labelColor = "black";
    this.errorBkgColor = "#552222";
    this.errorTextColor = "#552222";
    this.updateColors();
  }
  updateColors() {
    this.cScale0 = this.cScale0 || this.primaryColor;
    this.cScale1 = this.cScale1 || this.secondaryColor;
    this.cScale2 = this.cScale2 || this.tertiaryColor;
    this.cScale3 = this.cScale3 || adjust_default(this.primaryColor, { h: 30 });
    this.cScale4 = this.cScale4 || adjust_default(this.primaryColor, { h: 60 });
    this.cScale5 = this.cScale5 || adjust_default(this.primaryColor, { h: 90 });
    this.cScale6 = this.cScale6 || adjust_default(this.primaryColor, { h: 120 });
    this.cScale7 = this.cScale7 || adjust_default(this.primaryColor, { h: 150 });
    this.cScale8 = this.cScale8 || adjust_default(this.primaryColor, { h: 210 });
    this.cScale9 = this.cScale9 || adjust_default(this.primaryColor, { h: 270 });
    this.cScale10 = this.cScale10 || adjust_default(this.primaryColor, { h: 300 });
    this.cScale11 = this.cScale11 || adjust_default(this.primaryColor, { h: 330 });
    this["cScalePeer1"] = this["cScalePeer1"] || darken_default(this.secondaryColor, 45);
    this["cScalePeer2"] = this["cScalePeer2"] || darken_default(this.tertiaryColor, 40);
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScale" + i] = darken_default(this["cScale" + i], 10);
      this["cScalePeer" + i] = this["cScalePeer" + i] || darken_default(this["cScale" + i], 25);
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleInv" + i] = this["cScaleInv" + i] || adjust_default(this["cScale" + i], { h: 180 });
    }
    for (let i = 0; i < 5; i++) {
      this["surface" + i] = this["surface" + i] || adjust_default(this.mainBkg, { h: 30, l: -(5 + i * 5) });
      this["surfacePeer" + i] = this["surfacePeer" + i] || adjust_default(this.mainBkg, { h: 30, l: -(7 + i * 5) });
    }
    this.scaleLabelColor = this.scaleLabelColor !== "calculated" && this.scaleLabelColor ? this.scaleLabelColor : this.labelTextColor;
    if (this.labelTextColor !== "calculated") {
      this.cScaleLabel0 = this.cScaleLabel0 || invert_default(this.labelTextColor);
      this.cScaleLabel3 = this.cScaleLabel3 || invert_default(this.labelTextColor);
      for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
        this["cScaleLabel" + i] = this["cScaleLabel" + i] || this.labelTextColor;
      }
    }
    this.nodeBkg = this.mainBkg;
    this.nodeBorder = this.border1;
    this.clusterBkg = this.secondBkg;
    this.clusterBorder = this.border2;
    this.defaultLinkColor = this.lineColor;
    this.titleColor = this.textColor;
    this.edgeLabelBackground = this.labelBackground;
    this.actorBorder = lighten_default(this.border1, 23);
    this.actorBkg = this.mainBkg;
    this.labelBoxBkgColor = this.actorBkg;
    this.signalColor = this.textColor;
    this.signalTextColor = this.textColor;
    this.labelBoxBorderColor = this.actorBorder;
    this.labelTextColor = this.actorTextColor;
    this.loopTextColor = this.actorTextColor;
    this.noteBorderColor = this.border2;
    this.noteTextColor = this.actorTextColor;
    this.actorLineColor = this.actorBorder;
    this.taskTextColor = this.taskTextLightColor;
    this.taskTextOutsideColor = this.taskTextDarkColor;
    this.archEdgeColor = this.lineColor;
    this.archEdgeArrowColor = this.lineColor;
    this.rowOdd = this.rowOdd || lighten_default(this.primaryColor, 75) || "#ffffff";
    this.rowEven = this.rowEven || lighten_default(this.primaryColor, 1);
    this.transitionColor = this.transitionColor || this.lineColor;
    this.transitionLabelColor = this.transitionLabelColor || this.textColor;
    this.stateLabelColor = this.stateLabelColor || this.stateBkg || this.primaryTextColor;
    this.stateBkg = this.stateBkg || this.mainBkg;
    this.labelBackgroundColor = this.labelBackgroundColor || this.stateBkg;
    this.compositeBackground = this.compositeBackground || this.background || this.tertiaryColor;
    this.altBackground = this.altBackground || "#f0f0f0";
    this.compositeTitleBackground = this.compositeTitleBackground || this.mainBkg;
    this.compositeBorder = this.compositeBorder || this.nodeBorder;
    this.innerEndBackground = this.nodeBorder;
    this.specialStateColor = this.lineColor;
    this.errorBkgColor = this.errorBkgColor || this.tertiaryColor;
    this.errorTextColor = this.errorTextColor || this.tertiaryTextColor;
    this.transitionColor = this.transitionColor || this.lineColor;
    this.classText = this.primaryTextColor;
    this.fillType0 = this.primaryColor;
    this.fillType1 = this.secondaryColor;
    this.fillType2 = adjust_default(this.primaryColor, { h: 64 });
    this.fillType3 = adjust_default(this.secondaryColor, { h: 64 });
    this.fillType4 = adjust_default(this.primaryColor, { h: -64 });
    this.fillType5 = adjust_default(this.secondaryColor, { h: -64 });
    this.fillType6 = adjust_default(this.primaryColor, { h: 128 });
    this.fillType7 = adjust_default(this.secondaryColor, { h: 128 });
    this.pie1 = this.pie1 || this.primaryColor;
    this.pie2 = this.pie2 || this.secondaryColor;
    this.pie3 = this.pie3 || adjust_default(this.tertiaryColor, { l: -40 });
    this.pie4 = this.pie4 || adjust_default(this.primaryColor, { l: -10 });
    this.pie5 = this.pie5 || adjust_default(this.secondaryColor, { l: -30 });
    this.pie6 = this.pie6 || adjust_default(this.tertiaryColor, { l: -20 });
    this.pie7 = this.pie7 || adjust_default(this.primaryColor, { h: 60, l: -20 });
    this.pie8 = this.pie8 || adjust_default(this.primaryColor, { h: -60, l: -40 });
    this.pie9 = this.pie9 || adjust_default(this.primaryColor, { h: 120, l: -40 });
    this.pie10 = this.pie10 || adjust_default(this.primaryColor, { h: 60, l: -40 });
    this.pie11 = this.pie11 || adjust_default(this.primaryColor, { h: -90, l: -40 });
    this.pie12 = this.pie12 || adjust_default(this.primaryColor, { h: 120, l: -30 });
    this.pieTitleTextSize = this.pieTitleTextSize || "25px";
    this.pieTitleTextColor = this.pieTitleTextColor || this.taskTextDarkColor;
    this.pieSectionTextSize = this.pieSectionTextSize || "17px";
    this.pieSectionTextColor = this.pieSectionTextColor || this.textColor;
    this.pieLegendTextSize = this.pieLegendTextSize || "17px";
    this.pieLegendTextColor = this.pieLegendTextColor || this.taskTextDarkColor;
    this.pieStrokeColor = this.pieStrokeColor || "black";
    this.pieStrokeWidth = this.pieStrokeWidth || "2px";
    this.pieOuterStrokeWidth = this.pieOuterStrokeWidth || "2px";
    this.pieOuterStrokeColor = this.pieOuterStrokeColor || "black";
    this.pieOpacity = this.pieOpacity || "0.7";
    this.venn1 = this.venn1 ?? adjust_default(this.primaryColor, { l: -30 });
    this.venn2 = this.venn2 ?? adjust_default(this.secondaryColor, { l: -30 });
    this.venn3 = this.venn3 ?? adjust_default(this.tertiaryColor, { l: -40 });
    this.venn4 = this.venn4 ?? adjust_default(this.primaryColor, { h: 60, l: -30 });
    this.venn5 = this.venn5 ?? adjust_default(this.primaryColor, { h: -60, l: -30 });
    this.venn6 = this.venn6 ?? adjust_default(this.secondaryColor, { h: 60, l: -30 });
    this.venn7 = this.venn7 ?? adjust_default(this.primaryColor, { h: 120, l: -30 });
    this.venn8 = this.venn8 ?? adjust_default(this.secondaryColor, { h: 120, l: -30 });
    this.vennTitleTextColor = this.vennTitleTextColor ?? this.titleColor;
    this.vennSetTextColor = this.vennSetTextColor ?? this.textColor;
    this.quadrant1Fill = this.quadrant1Fill || this.primaryColor;
    this.quadrant2Fill = this.quadrant2Fill || adjust_default(this.primaryColor, { r: 5, g: 5, b: 5 });
    this.quadrant3Fill = this.quadrant3Fill || adjust_default(this.primaryColor, { r: 10, g: 10, b: 10 });
    this.quadrant4Fill = this.quadrant4Fill || adjust_default(this.primaryColor, { r: 15, g: 15, b: 15 });
    this.quadrant1TextFill = this.quadrant1TextFill || this.primaryTextColor;
    this.quadrant2TextFill = this.quadrant2TextFill || adjust_default(this.primaryTextColor, { r: -5, g: -5, b: -5 });
    this.quadrant3TextFill = this.quadrant3TextFill || adjust_default(this.primaryTextColor, { r: -10, g: -10, b: -10 });
    this.quadrant4TextFill = this.quadrant4TextFill || adjust_default(this.primaryTextColor, { r: -15, g: -15, b: -15 });
    this.quadrantPointFill = this.quadrantPointFill || is_dark_default(this.quadrant1Fill) ? lighten_default(this.quadrant1Fill) : darken_default(this.quadrant1Fill);
    this.quadrantPointTextFill = this.quadrantPointTextFill || this.primaryTextColor;
    this.quadrantXAxisTextFill = this.quadrantXAxisTextFill || this.primaryTextColor;
    this.quadrantYAxisTextFill = this.quadrantYAxisTextFill || this.primaryTextColor;
    this.quadrantInternalBorderStrokeFill = this.quadrantInternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantExternalBorderStrokeFill = this.quadrantExternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantTitleFill = this.quadrantTitleFill || this.primaryTextColor;
    this.radar = {
      axisColor: this.radar?.axisColor || this.lineColor,
      axisStrokeWidth: this.radar?.axisStrokeWidth || 2,
      axisLabelFontSize: this.radar?.axisLabelFontSize || 12,
      curveOpacity: this.radar?.curveOpacity || 0.5,
      curveStrokeWidth: this.radar?.curveStrokeWidth || 2,
      graticuleColor: this.radar?.graticuleColor || "#DEDEDE",
      graticuleStrokeWidth: this.radar?.graticuleStrokeWidth || 1,
      graticuleOpacity: this.radar?.graticuleOpacity || 0.3,
      legendBoxSize: this.radar?.legendBoxSize || 12,
      legendFontSize: this.radar?.legendFontSize || 12
    };
    this.xyChart = {
      backgroundColor: this.xyChart?.backgroundColor || this.background,
      titleColor: this.xyChart?.titleColor || this.primaryTextColor,
      xAxisTitleColor: this.xyChart?.xAxisTitleColor || this.primaryTextColor,
      xAxisLabelColor: this.xyChart?.xAxisLabelColor || this.primaryTextColor,
      xAxisTickColor: this.xyChart?.xAxisTickColor || this.primaryTextColor,
      xAxisLineColor: this.xyChart?.xAxisLineColor || this.primaryTextColor,
      yAxisTitleColor: this.xyChart?.yAxisTitleColor || this.primaryTextColor,
      yAxisLabelColor: this.xyChart?.yAxisLabelColor || this.primaryTextColor,
      yAxisTickColor: this.xyChart?.yAxisTickColor || this.primaryTextColor,
      yAxisLineColor: this.xyChart?.yAxisLineColor || this.primaryTextColor,
      plotColorPalette: this.xyChart?.plotColorPalette || "#ECECFF,#8493A6,#FFC3A0,#DCDDE1,#B8E994,#D1A36F,#C3CDE6,#FFB6C1,#496078,#F8F3E3"
    };
    this.requirementBackground = this.requirementBackground || this.primaryColor;
    this.requirementBorderColor = this.requirementBorderColor || this.primaryBorderColor;
    this.requirementBorderSize = this.requirementBorderSize || "1";
    this.requirementTextColor = this.requirementTextColor || this.primaryTextColor;
    this.relationColor = this.relationColor || this.lineColor;
    this.relationLabelBackground = this.relationLabelBackground || this.labelBackground;
    this.relationLabelColor = this.relationLabelColor || this.actorTextColor;
    this.git0 = this.git0 || this.primaryColor;
    this.git1 = this.git1 || this.secondaryColor;
    this.git2 = this.git2 || this.tertiaryColor;
    this.git3 = this.git3 || adjust_default(this.primaryColor, { h: -30 });
    this.git4 = this.git4 || adjust_default(this.primaryColor, { h: -60 });
    this.git5 = this.git5 || adjust_default(this.primaryColor, { h: -90 });
    this.git6 = this.git6 || adjust_default(this.primaryColor, { h: 60 });
    this.git7 = this.git7 || adjust_default(this.primaryColor, { h: 120 });
    if (this.darkMode) {
      this.git0 = lighten_default(this.git0, 25);
      this.git1 = lighten_default(this.git1, 25);
      this.git2 = lighten_default(this.git2, 25);
      this.git3 = lighten_default(this.git3, 25);
      this.git4 = lighten_default(this.git4, 25);
      this.git5 = lighten_default(this.git5, 25);
      this.git6 = lighten_default(this.git6, 25);
      this.git7 = lighten_default(this.git7, 25);
    } else {
      this.git0 = darken_default(this.git0, 25);
      this.git1 = darken_default(this.git1, 25);
      this.git2 = darken_default(this.git2, 25);
      this.git3 = darken_default(this.git3, 25);
      this.git4 = darken_default(this.git4, 25);
      this.git5 = darken_default(this.git5, 25);
      this.git6 = darken_default(this.git6, 25);
      this.git7 = darken_default(this.git7, 25);
    }
    this.gitInv0 = this.gitInv0 || darken_default(invert_default(this.git0), 25);
    this.gitInv1 = this.gitInv1 || invert_default(this.git1);
    this.gitInv2 = this.gitInv2 || invert_default(this.git2);
    this.gitInv3 = this.gitInv3 || invert_default(this.git3);
    this.gitInv4 = this.gitInv4 || invert_default(this.git4);
    this.gitInv5 = this.gitInv5 || invert_default(this.git5);
    this.gitInv6 = this.gitInv6 || invert_default(this.git6);
    this.gitInv7 = this.gitInv7 || invert_default(this.git7);
    this.gitBranchLabel0 = this.gitBranchLabel0 || invert_default(this.labelTextColor);
    this.gitBranchLabel1 = this.gitBranchLabel1 || this.labelTextColor;
    this.gitBranchLabel2 = this.gitBranchLabel2 || this.labelTextColor;
    this.gitBranchLabel3 = this.gitBranchLabel3 || invert_default(this.labelTextColor);
    this.gitBranchLabel4 = this.gitBranchLabel4 || this.labelTextColor;
    this.gitBranchLabel5 = this.gitBranchLabel5 || this.labelTextColor;
    this.gitBranchLabel6 = this.gitBranchLabel6 || this.labelTextColor;
    this.gitBranchLabel7 = this.gitBranchLabel7 || this.labelTextColor;
    this.tagLabelColor = this.tagLabelColor || this.primaryTextColor;
    this.tagLabelBackground = this.tagLabelBackground || this.primaryColor;
    this.tagLabelBorder = this.tagBorder || this.primaryBorderColor;
    this.tagLabelFontSize = this.tagLabelFontSize || "10px";
    this.commitLabelColor = this.commitLabelColor || this.secondaryTextColor;
    this.commitLabelBackground = this.commitLabelBackground || this.secondaryColor;
    this.commitLabelFontSize = this.commitLabelFontSize || "10px";
    this.attributeBackgroundColorOdd = this.attributeBackgroundColorOdd || oldAttributeBackgroundColorOdd;
    this.attributeBackgroundColorEven = this.attributeBackgroundColorEven || oldAttributeBackgroundColorEven;
  }
  calculate(overrides) {
    Object.keys(this).forEach((k) => {
      if (this[k] === "calculated") {
        this[k] = void 0;
      }
    });
    if (typeof overrides !== "object") {
      this.updateColors();
      return;
    }
    const keys = Object.keys(overrides);
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
    this.updateColors();
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
  }
}, __name(_a4, "Theme"), _a4);
var getThemeVariables3 = /* @__PURE__ */ __name((userOverrides) => {
  const theme = new Theme3();
  theme.calculate(userOverrides);
  return theme;
}, "getThemeVariables");
var _a5;
var Theme4 = (_a5 = class {
  constructor() {
    this.background = "#f4f4f4";
    this.primaryColor = "#cde498";
    this.secondaryColor = "#cdffb2";
    this.background = "white";
    this.mainBkg = "#cde498";
    this.secondBkg = "#cdffb2";
    this.lineColor = "green";
    this.border1 = "#13540c";
    this.border2 = "#6eaa49";
    this.arrowheadColor = "green";
    this.fontFamily = '"trebuchet ms", verdana, arial, sans-serif';
    this.fontSize = "16px";
    this.tertiaryColor = lighten_default("#cde498", 10);
    this.primaryBorderColor = mkBorder(this.primaryColor, this.darkMode);
    this.secondaryBorderColor = mkBorder(this.secondaryColor, this.darkMode);
    this.tertiaryBorderColor = mkBorder(this.tertiaryColor, this.darkMode);
    this.primaryTextColor = invert_default(this.primaryColor);
    this.secondaryTextColor = invert_default(this.secondaryColor);
    this.tertiaryTextColor = invert_default(this.primaryColor);
    this.lineColor = invert_default(this.background);
    this.textColor = invert_default(this.background);
    this.THEME_COLOR_LIMIT = 12;
    this.nodeBkg = "calculated";
    this.nodeBorder = "calculated";
    this.clusterBkg = "calculated";
    this.clusterBorder = "calculated";
    this.defaultLinkColor = "calculated";
    this.titleColor = "#333";
    this.edgeLabelBackground = "#e8e8e8";
    this.actorBorder = "calculated";
    this.actorBkg = "calculated";
    this.actorTextColor = "black";
    this.actorLineColor = "calculated";
    this.signalColor = "#333";
    this.signalTextColor = "#333";
    this.labelBoxBkgColor = "calculated";
    this.labelBoxBorderColor = "#326932";
    this.labelTextColor = "calculated";
    this.loopTextColor = "calculated";
    this.noteBorderColor = "calculated";
    this.noteBkgColor = "#fff5ad";
    this.noteTextColor = "calculated";
    this.activationBorderColor = "#666";
    this.activationBkgColor = "#f4f4f4";
    this.sequenceNumberColor = "white";
    this.sectionBkgColor = "#6eaa49";
    this.altSectionBkgColor = "white";
    this.sectionBkgColor2 = "#6eaa49";
    this.excludeBkgColor = "#eeeeee";
    this.taskBorderColor = "calculated";
    this.taskBkgColor = "#487e3a";
    this.taskTextLightColor = "white";
    this.taskTextColor = "calculated";
    this.taskTextDarkColor = "black";
    this.taskTextOutsideColor = "calculated";
    this.taskTextClickableColor = "#003163";
    this.activeTaskBorderColor = "calculated";
    this.activeTaskBkgColor = "calculated";
    this.gridColor = "lightgrey";
    this.doneTaskBkgColor = "lightgrey";
    this.doneTaskBorderColor = "grey";
    this.critBorderColor = "#ff8888";
    this.critBkgColor = "red";
    this.todayLineColor = "red";
    this.vertLineColor = "#00BFFF";
    this.personBorder = this.primaryBorderColor;
    this.personBkg = this.mainBkg;
    this.archEdgeColor = "calculated";
    this.archEdgeArrowColor = "calculated";
    this.archEdgeWidth = "3";
    this.archGroupBorderColor = this.primaryBorderColor;
    this.archGroupBorderWidth = "2px";
    this.labelColor = "black";
    this.errorBkgColor = "#552222";
    this.errorTextColor = "#552222";
  }
  updateColors() {
    this.actorBorder = darken_default(this.mainBkg, 20);
    this.actorBkg = this.mainBkg;
    this.labelBoxBkgColor = this.actorBkg;
    this.labelTextColor = this.actorTextColor;
    this.loopTextColor = this.actorTextColor;
    this.noteBorderColor = this.border2;
    this.noteTextColor = this.actorTextColor;
    this.actorLineColor = this.actorBorder;
    this.cScale0 = this.cScale0 || this.primaryColor;
    this.cScale1 = this.cScale1 || this.secondaryColor;
    this.cScale2 = this.cScale2 || this.tertiaryColor;
    this.cScale3 = this.cScale3 || adjust_default(this.primaryColor, { h: 30 });
    this.cScale4 = this.cScale4 || adjust_default(this.primaryColor, { h: 60 });
    this.cScale5 = this.cScale5 || adjust_default(this.primaryColor, { h: 90 });
    this.cScale6 = this.cScale6 || adjust_default(this.primaryColor, { h: 120 });
    this.cScale7 = this.cScale7 || adjust_default(this.primaryColor, { h: 150 });
    this.cScale8 = this.cScale8 || adjust_default(this.primaryColor, { h: 210 });
    this.cScale9 = this.cScale9 || adjust_default(this.primaryColor, { h: 270 });
    this.cScale10 = this.cScale10 || adjust_default(this.primaryColor, { h: 300 });
    this.cScale11 = this.cScale11 || adjust_default(this.primaryColor, { h: 330 });
    this["cScalePeer1"] = this["cScalePeer1"] || darken_default(this.secondaryColor, 45);
    this["cScalePeer2"] = this["cScalePeer2"] || darken_default(this.tertiaryColor, 40);
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScale" + i] = darken_default(this["cScale" + i], 10);
      this["cScalePeer" + i] = this["cScalePeer" + i] || darken_default(this["cScale" + i], 25);
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleInv" + i] = this["cScaleInv" + i] || adjust_default(this["cScale" + i], { h: 180 });
    }
    this.scaleLabelColor = this.scaleLabelColor !== "calculated" && this.scaleLabelColor ? this.scaleLabelColor : this.labelTextColor;
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleLabel" + i] = this["cScaleLabel" + i] || this.scaleLabelColor;
    }
    for (let i = 0; i < 5; i++) {
      this["surface" + i] = this["surface" + i] || adjust_default(this.mainBkg, { h: 30, s: -30, l: -(5 + i * 5) });
      this["surfacePeer" + i] = this["surfacePeer" + i] || adjust_default(this.mainBkg, { h: 30, s: -30, l: -(8 + i * 5) });
    }
    this.nodeBkg = this.mainBkg;
    this.nodeBorder = this.border1;
    this.clusterBkg = this.secondBkg;
    this.clusterBorder = this.border2;
    this.defaultLinkColor = this.lineColor;
    this.taskBorderColor = this.border1;
    this.taskTextColor = this.taskTextLightColor;
    this.taskTextOutsideColor = this.taskTextDarkColor;
    this.activeTaskBorderColor = this.taskBorderColor;
    this.activeTaskBkgColor = this.mainBkg;
    this.archEdgeColor = this.lineColor;
    this.archEdgeArrowColor = this.lineColor;
    this.rowOdd = this.rowOdd || lighten_default(this.mainBkg, 75) || "#ffffff";
    this.rowEven = this.rowEven || lighten_default(this.mainBkg, 20);
    this.transitionColor = this.transitionColor || this.lineColor;
    this.transitionLabelColor = this.transitionLabelColor || this.textColor;
    this.stateLabelColor = this.stateLabelColor || this.stateBkg || this.primaryTextColor;
    this.stateBkg = this.stateBkg || this.mainBkg;
    this.labelBackgroundColor = this.labelBackgroundColor || this.stateBkg;
    this.compositeBackground = this.compositeBackground || this.background || this.tertiaryColor;
    this.altBackground = this.altBackground || "#f0f0f0";
    this.compositeTitleBackground = this.compositeTitleBackground || this.mainBkg;
    this.compositeBorder = this.compositeBorder || this.nodeBorder;
    this.innerEndBackground = this.primaryBorderColor;
    this.specialStateColor = this.lineColor;
    this.errorBkgColor = this.errorBkgColor || this.tertiaryColor;
    this.errorTextColor = this.errorTextColor || this.tertiaryTextColor;
    this.transitionColor = this.transitionColor || this.lineColor;
    this.classText = this.primaryTextColor;
    this.fillType0 = this.primaryColor;
    this.fillType1 = this.secondaryColor;
    this.fillType2 = adjust_default(this.primaryColor, { h: 64 });
    this.fillType3 = adjust_default(this.secondaryColor, { h: 64 });
    this.fillType4 = adjust_default(this.primaryColor, { h: -64 });
    this.fillType5 = adjust_default(this.secondaryColor, { h: -64 });
    this.fillType6 = adjust_default(this.primaryColor, { h: 128 });
    this.fillType7 = adjust_default(this.secondaryColor, { h: 128 });
    this.pie1 = this.pie1 || this.primaryColor;
    this.pie2 = this.pie2 || this.secondaryColor;
    this.pie3 = this.pie3 || this.tertiaryColor;
    this.pie4 = this.pie4 || adjust_default(this.primaryColor, { l: -30 });
    this.pie5 = this.pie5 || adjust_default(this.secondaryColor, { l: -30 });
    this.pie6 = this.pie6 || adjust_default(this.tertiaryColor, { h: 40, l: -40 });
    this.pie7 = this.pie7 || adjust_default(this.primaryColor, { h: 60, l: -10 });
    this.pie8 = this.pie8 || adjust_default(this.primaryColor, { h: -60, l: -10 });
    this.pie9 = this.pie9 || adjust_default(this.primaryColor, { h: 120, l: 0 });
    this.pie10 = this.pie10 || adjust_default(this.primaryColor, { h: 60, l: -50 });
    this.pie11 = this.pie11 || adjust_default(this.primaryColor, { h: -60, l: -50 });
    this.pie12 = this.pie12 || adjust_default(this.primaryColor, { h: 120, l: -50 });
    this.pieTitleTextSize = this.pieTitleTextSize || "25px";
    this.pieTitleTextColor = this.pieTitleTextColor || this.taskTextDarkColor;
    this.pieSectionTextSize = this.pieSectionTextSize || "17px";
    this.pieSectionTextColor = this.pieSectionTextColor || this.textColor;
    this.pieLegendTextSize = this.pieLegendTextSize || "17px";
    this.pieLegendTextColor = this.pieLegendTextColor || this.taskTextDarkColor;
    this.pieStrokeColor = this.pieStrokeColor || "black";
    this.pieStrokeWidth = this.pieStrokeWidth || "2px";
    this.pieOuterStrokeWidth = this.pieOuterStrokeWidth || "2px";
    this.pieOuterStrokeColor = this.pieOuterStrokeColor || "black";
    this.pieOpacity = this.pieOpacity || "0.7";
    this.venn1 = this.venn1 ?? adjust_default(this.primaryColor, { l: -30 });
    this.venn2 = this.venn2 ?? adjust_default(this.secondaryColor, { l: -30 });
    this.venn3 = this.venn3 ?? adjust_default(this.tertiaryColor, { l: -30 });
    this.venn4 = this.venn4 ?? adjust_default(this.primaryColor, { h: 60, l: -30 });
    this.venn5 = this.venn5 ?? adjust_default(this.primaryColor, { h: -60, l: -30 });
    this.venn6 = this.venn6 ?? adjust_default(this.secondaryColor, { h: 60, l: -30 });
    this.venn7 = this.venn7 ?? adjust_default(this.primaryColor, { h: 120, l: -30 });
    this.venn8 = this.venn8 ?? adjust_default(this.secondaryColor, { h: 120, l: -30 });
    this.vennTitleTextColor = this.vennTitleTextColor ?? this.titleColor;
    this.vennSetTextColor = this.vennSetTextColor ?? this.textColor;
    this.quadrant1Fill = this.quadrant1Fill || this.primaryColor;
    this.quadrant2Fill = this.quadrant2Fill || adjust_default(this.primaryColor, { r: 5, g: 5, b: 5 });
    this.quadrant3Fill = this.quadrant3Fill || adjust_default(this.primaryColor, { r: 10, g: 10, b: 10 });
    this.quadrant4Fill = this.quadrant4Fill || adjust_default(this.primaryColor, { r: 15, g: 15, b: 15 });
    this.quadrant1TextFill = this.quadrant1TextFill || this.primaryTextColor;
    this.quadrant2TextFill = this.quadrant2TextFill || adjust_default(this.primaryTextColor, { r: -5, g: -5, b: -5 });
    this.quadrant3TextFill = this.quadrant3TextFill || adjust_default(this.primaryTextColor, { r: -10, g: -10, b: -10 });
    this.quadrant4TextFill = this.quadrant4TextFill || adjust_default(this.primaryTextColor, { r: -15, g: -15, b: -15 });
    this.quadrantPointFill = this.quadrantPointFill || is_dark_default(this.quadrant1Fill) ? lighten_default(this.quadrant1Fill) : darken_default(this.quadrant1Fill);
    this.quadrantPointTextFill = this.quadrantPointTextFill || this.primaryTextColor;
    this.quadrantXAxisTextFill = this.quadrantXAxisTextFill || this.primaryTextColor;
    this.quadrantYAxisTextFill = this.quadrantYAxisTextFill || this.primaryTextColor;
    this.quadrantInternalBorderStrokeFill = this.quadrantInternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantExternalBorderStrokeFill = this.quadrantExternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantTitleFill = this.quadrantTitleFill || this.primaryTextColor;
    this.packet = {
      startByteColor: this.primaryTextColor,
      endByteColor: this.primaryTextColor,
      labelColor: this.primaryTextColor,
      titleColor: this.primaryTextColor,
      blockStrokeColor: this.primaryTextColor,
      blockFillColor: this.mainBkg
    };
    this.radar = {
      axisColor: this.radar?.axisColor || this.lineColor,
      axisStrokeWidth: this.radar?.axisStrokeWidth || 2,
      axisLabelFontSize: this.radar?.axisLabelFontSize || 12,
      curveOpacity: this.radar?.curveOpacity || 0.5,
      curveStrokeWidth: this.radar?.curveStrokeWidth || 2,
      graticuleColor: this.radar?.graticuleColor || "#DEDEDE",
      graticuleStrokeWidth: this.radar?.graticuleStrokeWidth || 1,
      graticuleOpacity: this.radar?.graticuleOpacity || 0.3,
      legendBoxSize: this.radar?.legendBoxSize || 12,
      legendFontSize: this.radar?.legendFontSize || 12
    };
    this.xyChart = {
      backgroundColor: this.xyChart?.backgroundColor || this.background,
      titleColor: this.xyChart?.titleColor || this.primaryTextColor,
      xAxisTitleColor: this.xyChart?.xAxisTitleColor || this.primaryTextColor,
      xAxisLabelColor: this.xyChart?.xAxisLabelColor || this.primaryTextColor,
      xAxisTickColor: this.xyChart?.xAxisTickColor || this.primaryTextColor,
      xAxisLineColor: this.xyChart?.xAxisLineColor || this.primaryTextColor,
      yAxisTitleColor: this.xyChart?.yAxisTitleColor || this.primaryTextColor,
      yAxisLabelColor: this.xyChart?.yAxisLabelColor || this.primaryTextColor,
      yAxisTickColor: this.xyChart?.yAxisTickColor || this.primaryTextColor,
      yAxisLineColor: this.xyChart?.yAxisLineColor || this.primaryTextColor,
      plotColorPalette: this.xyChart?.plotColorPalette || "#CDE498,#FF6B6B,#A0D2DB,#D7BDE2,#F0F0F0,#FFC3A0,#7FD8BE,#FF9A8B,#FAF3E0,#FFF176"
    };
    this.requirementBackground = this.requirementBackground || this.primaryColor;
    this.requirementBorderColor = this.requirementBorderColor || this.primaryBorderColor;
    this.requirementBorderSize = this.requirementBorderSize || "1";
    this.requirementTextColor = this.requirementTextColor || this.primaryTextColor;
    this.relationColor = this.relationColor || this.lineColor;
    this.relationLabelBackground = this.relationLabelBackground || this.edgeLabelBackground;
    this.relationLabelColor = this.relationLabelColor || this.actorTextColor;
    this.git0 = this.git0 || this.primaryColor;
    this.git1 = this.git1 || this.secondaryColor;
    this.git2 = this.git2 || this.tertiaryColor;
    this.git3 = this.git3 || adjust_default(this.primaryColor, { h: -30 });
    this.git4 = this.git4 || adjust_default(this.primaryColor, { h: -60 });
    this.git5 = this.git5 || adjust_default(this.primaryColor, { h: -90 });
    this.git6 = this.git6 || adjust_default(this.primaryColor, { h: 60 });
    this.git7 = this.git7 || adjust_default(this.primaryColor, { h: 120 });
    if (this.darkMode) {
      this.git0 = lighten_default(this.git0, 25);
      this.git1 = lighten_default(this.git1, 25);
      this.git2 = lighten_default(this.git2, 25);
      this.git3 = lighten_default(this.git3, 25);
      this.git4 = lighten_default(this.git4, 25);
      this.git5 = lighten_default(this.git5, 25);
      this.git6 = lighten_default(this.git6, 25);
      this.git7 = lighten_default(this.git7, 25);
    } else {
      this.git0 = darken_default(this.git0, 25);
      this.git1 = darken_default(this.git1, 25);
      this.git2 = darken_default(this.git2, 25);
      this.git3 = darken_default(this.git3, 25);
      this.git4 = darken_default(this.git4, 25);
      this.git5 = darken_default(this.git5, 25);
      this.git6 = darken_default(this.git6, 25);
      this.git7 = darken_default(this.git7, 25);
    }
    this.gitInv0 = this.gitInv0 || invert_default(this.git0);
    this.gitInv1 = this.gitInv1 || invert_default(this.git1);
    this.gitInv2 = this.gitInv2 || invert_default(this.git2);
    this.gitInv3 = this.gitInv3 || invert_default(this.git3);
    this.gitInv4 = this.gitInv4 || invert_default(this.git4);
    this.gitInv5 = this.gitInv5 || invert_default(this.git5);
    this.gitInv6 = this.gitInv6 || invert_default(this.git6);
    this.gitInv7 = this.gitInv7 || invert_default(this.git7);
    this.gitBranchLabel0 = this.gitBranchLabel0 || invert_default(this.labelTextColor);
    this.gitBranchLabel1 = this.gitBranchLabel1 || this.labelTextColor;
    this.gitBranchLabel2 = this.gitBranchLabel2 || this.labelTextColor;
    this.gitBranchLabel3 = this.gitBranchLabel3 || invert_default(this.labelTextColor);
    this.gitBranchLabel4 = this.gitBranchLabel4 || this.labelTextColor;
    this.gitBranchLabel5 = this.gitBranchLabel5 || this.labelTextColor;
    this.gitBranchLabel6 = this.gitBranchLabel6 || this.labelTextColor;
    this.gitBranchLabel7 = this.gitBranchLabel7 || this.labelTextColor;
    this.tagLabelColor = this.tagLabelColor || this.primaryTextColor;
    this.tagLabelBackground = this.tagLabelBackground || this.primaryColor;
    this.tagLabelBorder = this.tagBorder || this.primaryBorderColor;
    this.tagLabelFontSize = this.tagLabelFontSize || "10px";
    this.commitLabelColor = this.commitLabelColor || this.secondaryTextColor;
    this.commitLabelBackground = this.commitLabelBackground || this.secondaryColor;
    this.commitLabelFontSize = this.commitLabelFontSize || "10px";
    this.attributeBackgroundColorOdd = this.attributeBackgroundColorOdd || oldAttributeBackgroundColorOdd;
    this.attributeBackgroundColorEven = this.attributeBackgroundColorEven || oldAttributeBackgroundColorEven;
  }
  calculate(overrides) {
    if (typeof overrides !== "object") {
      this.updateColors();
      return;
    }
    const keys = Object.keys(overrides);
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
    this.updateColors();
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
  }
}, __name(_a5, "Theme"), _a5);
var getThemeVariables4 = /* @__PURE__ */ __name((userOverrides) => {
  const theme = new Theme4();
  theme.calculate(userOverrides);
  return theme;
}, "getThemeVariables");
var _a6;
var Theme5 = (_a6 = class {
  constructor() {
    this.primaryColor = "#eee";
    this.contrast = "#707070";
    this.secondaryColor = lighten_default(this.contrast, 55);
    this.background = "#ffffff";
    this.tertiaryColor = adjust_default(this.primaryColor, { h: -160 });
    this.primaryBorderColor = mkBorder(this.primaryColor, this.darkMode);
    this.secondaryBorderColor = mkBorder(this.secondaryColor, this.darkMode);
    this.tertiaryBorderColor = mkBorder(this.tertiaryColor, this.darkMode);
    this.primaryTextColor = invert_default(this.primaryColor);
    this.secondaryTextColor = invert_default(this.secondaryColor);
    this.tertiaryTextColor = invert_default(this.tertiaryColor);
    this.lineColor = invert_default(this.background);
    this.textColor = invert_default(this.background);
    this.mainBkg = "#eee";
    this.secondBkg = "calculated";
    this.lineColor = "#666";
    this.border1 = "#999";
    this.border2 = "calculated";
    this.note = "#ffa";
    this.text = "#333";
    this.critical = "#d42";
    this.done = "#bbb";
    this.arrowheadColor = "#333333";
    this.fontFamily = '"trebuchet ms", verdana, arial, sans-serif';
    this.fontSize = "16px";
    this.THEME_COLOR_LIMIT = 12;
    this.nodeBkg = "calculated";
    this.nodeBorder = "calculated";
    this.clusterBkg = "calculated";
    this.clusterBorder = "calculated";
    this.defaultLinkColor = "calculated";
    this.titleColor = "calculated";
    this.edgeLabelBackground = "white";
    this.actorBorder = "calculated";
    this.actorBkg = "calculated";
    this.actorTextColor = "calculated";
    this.actorLineColor = this.actorBorder;
    this.signalColor = "calculated";
    this.signalTextColor = "calculated";
    this.labelBoxBkgColor = "calculated";
    this.labelBoxBorderColor = "calculated";
    this.labelTextColor = "calculated";
    this.loopTextColor = "calculated";
    this.noteBorderColor = "calculated";
    this.noteBkgColor = "calculated";
    this.noteTextColor = "calculated";
    this.activationBorderColor = "#666";
    this.activationBkgColor = "#f4f4f4";
    this.sequenceNumberColor = "white";
    this.sectionBkgColor = "calculated";
    this.altSectionBkgColor = "white";
    this.sectionBkgColor2 = "calculated";
    this.excludeBkgColor = "#eeeeee";
    this.taskBorderColor = "calculated";
    this.taskBkgColor = "calculated";
    this.taskTextLightColor = "white";
    this.taskTextColor = "calculated";
    this.taskTextDarkColor = "calculated";
    this.taskTextOutsideColor = "calculated";
    this.taskTextClickableColor = "#003163";
    this.activeTaskBorderColor = "calculated";
    this.activeTaskBkgColor = "calculated";
    this.gridColor = "calculated";
    this.doneTaskBkgColor = "calculated";
    this.doneTaskBorderColor = "calculated";
    this.critBkgColor = "calculated";
    this.critBorderColor = "calculated";
    this.todayLineColor = "calculated";
    this.vertLineColor = "calculated";
    this.personBorder = this.primaryBorderColor;
    this.personBkg = this.mainBkg;
    this.archEdgeColor = "calculated";
    this.archEdgeArrowColor = "calculated";
    this.archEdgeWidth = "3";
    this.archGroupBorderColor = this.primaryBorderColor;
    this.archGroupBorderWidth = "2px";
    this.rowOdd = this.rowOdd || lighten_default(this.mainBkg, 75) || "#ffffff";
    this.rowEven = this.rowEven || "#f4f4f4";
    this.labelColor = "black";
    this.errorBkgColor = "#552222";
    this.errorTextColor = "#552222";
  }
  updateColors() {
    this.secondBkg = lighten_default(this.contrast, 55);
    this.border2 = this.contrast;
    this.actorBorder = lighten_default(this.border1, 23);
    this.actorBkg = this.mainBkg;
    this.actorTextColor = this.text;
    this.actorLineColor = this.actorBorder;
    this.signalColor = this.text;
    this.signalTextColor = this.text;
    this.labelBoxBkgColor = this.actorBkg;
    this.labelBoxBorderColor = this.actorBorder;
    this.labelTextColor = this.text;
    this.loopTextColor = this.text;
    this.noteBorderColor = "#999";
    this.noteBkgColor = "#666";
    this.noteTextColor = "#fff";
    this.cScale0 = this.cScale0 || "#555";
    this.cScale1 = this.cScale1 || "#F4F4F4";
    this.cScale2 = this.cScale2 || "#555";
    this.cScale3 = this.cScale3 || "#BBB";
    this.cScale4 = this.cScale4 || "#777";
    this.cScale5 = this.cScale5 || "#999";
    this.cScale6 = this.cScale6 || "#DDD";
    this.cScale7 = this.cScale7 || "#FFF";
    this.cScale8 = this.cScale8 || "#DDD";
    this.cScale9 = this.cScale9 || "#BBB";
    this.cScale10 = this.cScale10 || "#999";
    this.cScale11 = this.cScale11 || "#777";
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleInv" + i] = this["cScaleInv" + i] || invert_default(this["cScale" + i]);
    }
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      if (this.darkMode) {
        this["cScalePeer" + i] = this["cScalePeer" + i] || lighten_default(this["cScale" + i], 10);
      } else {
        this["cScalePeer" + i] = this["cScalePeer" + i] || darken_default(this["cScale" + i], 10);
      }
    }
    this.scaleLabelColor = this.scaleLabelColor || (this.darkMode ? "black" : this.labelTextColor);
    this.cScaleLabel0 = this.cScaleLabel0 || this.cScale1;
    this.cScaleLabel2 = this.cScaleLabel2 || this.cScale1;
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["cScaleLabel" + i] = this["cScaleLabel" + i] || this.scaleLabelColor;
    }
    for (let i = 0; i < 5; i++) {
      this["surface" + i] = this["surface" + i] || adjust_default(this.mainBkg, { l: -(5 + i * 5) });
      this["surfacePeer" + i] = this["surfacePeer" + i] || adjust_default(this.mainBkg, { l: -(8 + i * 5) });
    }
    this.nodeBkg = this.mainBkg;
    this.nodeBorder = this.border1;
    this.clusterBkg = this.secondBkg;
    this.clusterBorder = this.border2;
    this.defaultLinkColor = this.lineColor;
    this.titleColor = this.text;
    this.sectionBkgColor = lighten_default(this.contrast, 30);
    this.sectionBkgColor2 = lighten_default(this.contrast, 30);
    this.taskBorderColor = darken_default(this.contrast, 10);
    this.taskBkgColor = this.contrast;
    this.taskTextColor = this.taskTextLightColor;
    this.taskTextDarkColor = this.text;
    this.taskTextOutsideColor = this.taskTextDarkColor;
    this.activeTaskBorderColor = this.taskBorderColor;
    this.activeTaskBkgColor = this.mainBkg;
    this.gridColor = lighten_default(this.border1, 30);
    this.doneTaskBkgColor = this.done;
    this.doneTaskBorderColor = this.lineColor;
    this.critBkgColor = this.critical;
    this.critBorderColor = darken_default(this.critBkgColor, 10);
    this.todayLineColor = this.critBkgColor;
    this.vertLineColor = this.critBkgColor;
    this.archEdgeColor = this.lineColor;
    this.archEdgeArrowColor = this.lineColor;
    this.transitionColor = this.transitionColor || "#000";
    this.transitionLabelColor = this.transitionLabelColor || this.textColor;
    this.stateLabelColor = this.stateLabelColor || this.stateBkg || this.primaryTextColor;
    this.stateBkg = this.stateBkg || this.mainBkg;
    this.labelBackgroundColor = this.labelBackgroundColor || this.stateBkg;
    this.compositeBackground = this.compositeBackground || this.background || this.tertiaryColor;
    this.altBackground = this.altBackground || "#f4f4f4";
    this.compositeTitleBackground = this.compositeTitleBackground || this.mainBkg;
    this.stateBorder = this.stateBorder || "#000";
    this.innerEndBackground = this.primaryBorderColor;
    this.specialStateColor = "#222";
    this.errorBkgColor = this.errorBkgColor || this.tertiaryColor;
    this.errorTextColor = this.errorTextColor || this.tertiaryTextColor;
    this.classText = this.primaryTextColor;
    this.fillType0 = this.primaryColor;
    this.fillType1 = this.secondaryColor;
    this.fillType2 = adjust_default(this.primaryColor, { h: 64 });
    this.fillType3 = adjust_default(this.secondaryColor, { h: 64 });
    this.fillType4 = adjust_default(this.primaryColor, { h: -64 });
    this.fillType5 = adjust_default(this.secondaryColor, { h: -64 });
    this.fillType6 = adjust_default(this.primaryColor, { h: 128 });
    this.fillType7 = adjust_default(this.secondaryColor, { h: 128 });
    for (let i = 0; i < this.THEME_COLOR_LIMIT; i++) {
      this["pie" + i] = this["cScale" + i];
    }
    this.pie12 = this.pie0;
    this.pieTitleTextSize = this.pieTitleTextSize || "25px";
    this.pieTitleTextColor = this.pieTitleTextColor || this.taskTextDarkColor;
    this.pieSectionTextSize = this.pieSectionTextSize || "17px";
    this.pieSectionTextColor = this.pieSectionTextColor || this.textColor;
    this.pieLegendTextSize = this.pieLegendTextSize || "17px";
    this.pieLegendTextColor = this.pieLegendTextColor || this.taskTextDarkColor;
    this.pieStrokeColor = this.pieStrokeColor || "black";
    this.pieStrokeWidth = this.pieStrokeWidth || "2px";
    this.pieOuterStrokeWidth = this.pieOuterStrokeWidth || "2px";
    this.pieOuterStrokeColor = this.pieOuterStrokeColor || "black";
    this.pieOpacity = this.pieOpacity || "0.7";
    for (let i = 0; i < 8; i++) {
      this["venn" + (i + 1)] = this["venn" + (i + 1)] ?? this["cScale" + i];
    }
    this.vennTitleTextColor = this.vennTitleTextColor ?? this.titleColor;
    this.vennSetTextColor = this.vennSetTextColor ?? this.textColor;
    this.quadrant1Fill = this.quadrant1Fill || this.primaryColor;
    this.quadrant2Fill = this.quadrant2Fill || adjust_default(this.primaryColor, { r: 5, g: 5, b: 5 });
    this.quadrant3Fill = this.quadrant3Fill || adjust_default(this.primaryColor, { r: 10, g: 10, b: 10 });
    this.quadrant4Fill = this.quadrant4Fill || adjust_default(this.primaryColor, { r: 15, g: 15, b: 15 });
    this.quadrant1TextFill = this.quadrant1TextFill || this.primaryTextColor;
    this.quadrant2TextFill = this.quadrant2TextFill || adjust_default(this.primaryTextColor, { r: -5, g: -5, b: -5 });
    this.quadrant3TextFill = this.quadrant3TextFill || adjust_default(this.primaryTextColor, { r: -10, g: -10, b: -10 });
    this.quadrant4TextFill = this.quadrant4TextFill || adjust_default(this.primaryTextColor, { r: -15, g: -15, b: -15 });
    this.quadrantPointFill = this.quadrantPointFill || is_dark_default(this.quadrant1Fill) ? lighten_default(this.quadrant1Fill) : darken_default(this.quadrant1Fill);
    this.quadrantPointTextFill = this.quadrantPointTextFill || this.primaryTextColor;
    this.quadrantXAxisTextFill = this.quadrantXAxisTextFill || this.primaryTextColor;
    this.quadrantYAxisTextFill = this.quadrantYAxisTextFill || this.primaryTextColor;
    this.quadrantInternalBorderStrokeFill = this.quadrantInternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantExternalBorderStrokeFill = this.quadrantExternalBorderStrokeFill || this.primaryBorderColor;
    this.quadrantTitleFill = this.quadrantTitleFill || this.primaryTextColor;
    this.xyChart = {
      backgroundColor: this.xyChart?.backgroundColor || this.background,
      titleColor: this.xyChart?.titleColor || this.primaryTextColor,
      xAxisTitleColor: this.xyChart?.xAxisTitleColor || this.primaryTextColor,
      xAxisLabelColor: this.xyChart?.xAxisLabelColor || this.primaryTextColor,
      xAxisTickColor: this.xyChart?.xAxisTickColor || this.primaryTextColor,
      xAxisLineColor: this.xyChart?.xAxisLineColor || this.primaryTextColor,
      yAxisTitleColor: this.xyChart?.yAxisTitleColor || this.primaryTextColor,
      yAxisLabelColor: this.xyChart?.yAxisLabelColor || this.primaryTextColor,
      yAxisTickColor: this.xyChart?.yAxisTickColor || this.primaryTextColor,
      yAxisLineColor: this.xyChart?.yAxisLineColor || this.primaryTextColor,
      plotColorPalette: this.xyChart?.plotColorPalette || "#EEE,#6BB8E4,#8ACB88,#C7ACD6,#E8DCC2,#FFB2A8,#FFF380,#7E8D91,#FFD8B1,#FAF3E0"
    };
    this.radar = {
      axisColor: this.radar?.axisColor || this.lineColor,
      axisStrokeWidth: this.radar?.axisStrokeWidth || 2,
      axisLabelFontSize: this.radar?.axisLabelFontSize || 12,
      curveOpacity: this.radar?.curveOpacity || 0.5,
      curveStrokeWidth: this.radar?.curveStrokeWidth || 2,
      graticuleColor: this.radar?.graticuleColor || "#DEDEDE",
      graticuleStrokeWidth: this.radar?.graticuleStrokeWidth || 1,
      graticuleOpacity: this.radar?.graticuleOpacity || 0.3,
      legendBoxSize: this.radar?.legendBoxSize || 12,
      legendFontSize: this.radar?.legendFontSize || 12
    };
    this.requirementBackground = this.requirementBackground || this.primaryColor;
    this.requirementBorderColor = this.requirementBorderColor || this.primaryBorderColor;
    this.requirementBorderSize = this.requirementBorderSize || "1";
    this.requirementTextColor = this.requirementTextColor || this.primaryTextColor;
    this.relationColor = this.relationColor || this.lineColor;
    this.relationLabelBackground = this.relationLabelBackground || this.edgeLabelBackground;
    this.relationLabelColor = this.relationLabelColor || this.actorTextColor;
    this.git0 = darken_default(this.pie1, 25) || this.primaryColor;
    this.git1 = this.pie2 || this.secondaryColor;
    this.git2 = this.pie3 || this.tertiaryColor;
    this.git3 = this.pie4 || adjust_default(this.primaryColor, { h: -30 });
    this.git4 = this.pie5 || adjust_default(this.primaryColor, { h: -60 });
    this.git5 = this.pie6 || adjust_default(this.primaryColor, { h: -90 });
    this.git6 = this.pie7 || adjust_default(this.primaryColor, { h: 60 });
    this.git7 = this.pie8 || adjust_default(this.primaryColor, { h: 120 });
    this.gitInv0 = this.gitInv0 || invert_default(this.git0);
    this.gitInv1 = this.gitInv1 || invert_default(this.git1);
    this.gitInv2 = this.gitInv2 || invert_default(this.git2);
    this.gitInv3 = this.gitInv3 || invert_default(this.git3);
    this.gitInv4 = this.gitInv4 || invert_default(this.git4);
    this.gitInv5 = this.gitInv5 || invert_default(this.git5);
    this.gitInv6 = this.gitInv6 || invert_default(this.git6);
    this.gitInv7 = this.gitInv7 || invert_default(this.git7);
    this.branchLabelColor = this.branchLabelColor || this.labelTextColor;
    this.gitBranchLabel0 = this.branchLabelColor;
    this.gitBranchLabel1 = "white";
    this.gitBranchLabel2 = this.branchLabelColor;
    this.gitBranchLabel3 = "white";
    this.gitBranchLabel4 = this.branchLabelColor;
    this.gitBranchLabel5 = this.branchLabelColor;
    this.gitBranchLabel6 = this.branchLabelColor;
    this.gitBranchLabel7 = this.branchLabelColor;
    this.tagLabelColor = this.tagLabelColor || this.primaryTextColor;
    this.tagLabelBackground = this.tagLabelBackground || this.primaryColor;
    this.tagLabelBorder = this.tagBorder || this.primaryBorderColor;
    this.tagLabelFontSize = this.tagLabelFontSize || "10px";
    this.commitLabelColor = this.commitLabelColor || this.secondaryTextColor;
    this.commitLabelBackground = this.commitLabelBackground || this.secondaryColor;
    this.commitLabelFontSize = this.commitLabelFontSize || "10px";
    this.attributeBackgroundColorOdd = this.attributeBackgroundColorOdd || oldAttributeBackgroundColorOdd;
    this.attributeBackgroundColorEven = this.attributeBackgroundColorEven || oldAttributeBackgroundColorEven;
  }
  calculate(overrides) {
    if (typeof overrides !== "object") {
      this.updateColors();
      return;
    }
    const keys = Object.keys(overrides);
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
    this.updateColors();
    keys.forEach((k) => {
      this[k] = overrides[k];
    });
  }
}, __name(_a6, "Theme"), _a6);
var getThemeVariables5 = /* @__PURE__ */ __name((userOverrides) => {
  const theme = new Theme5();
  theme.calculate(userOverrides);
  return theme;
}, "getThemeVariables");
var themes_default = {
  base: {
    getThemeVariables
  },
  dark: {
    getThemeVariables: getThemeVariables2
  },
  default: {
    getThemeVariables: getThemeVariables3
  },
  forest: {
    getThemeVariables: getThemeVariables4
  },
  neutral: {
    getThemeVariables: getThemeVariables5
  }
};
var config_schema_default = {
  "flowchart": {
    "useMaxWidth": true,
    "titleTopMargin": 25,
    "subGraphTitleMargin": {
      "top": 0,
      "bottom": 0
    },
    "diagramPadding": 8,
    "htmlLabels": null,
    "nodeSpacing": 50,
    "rankSpacing": 50,
    "curve": "basis",
    "padding": 15,
    "defaultRenderer": "dagre-wrapper",
    "wrappingWidth": 200,
    "inheritDir": false
  },
  "sequence": {
    "useMaxWidth": true,
    "hideUnusedParticipants": false,
    "activationWidth": 10,
    "diagramMarginX": 50,
    "diagramMarginY": 10,
    "actorMargin": 50,
    "width": 150,
    "height": 65,
    "boxMargin": 10,
    "boxTextMargin": 5,
    "noteMargin": 10,
    "messageMargin": 35,
    "messageAlign": "center",
    "mirrorActors": true,
    "forceMenus": false,
    "bottomMarginAdj": 1,
    "rightAngles": false,
    "showSequenceNumbers": false,
    "actorFontSize": 14,
    "actorFontFamily": '"Open Sans", sans-serif',
    "actorFontWeight": 400,
    "noteFontSize": 14,
    "noteFontFamily": '"trebuchet ms", verdana, arial, sans-serif',
    "noteFontWeight": 400,
    "noteAlign": "center",
    "messageFontSize": 16,
    "messageFontFamily": '"trebuchet ms", verdana, arial, sans-serif',
    "messageFontWeight": 400,
    "wrap": false,
    "wrapPadding": 10,
    "labelBoxWidth": 50,
    "labelBoxHeight": 20
  },
  "gantt": {
    "useMaxWidth": true,
    "titleTopMargin": 25,
    "barHeight": 20,
    "barGap": 4,
    "topPadding": 50,
    "rightPadding": 75,
    "leftPadding": 75,
    "gridLineStartPadding": 35,
    "fontSize": 11,
    "sectionFontSize": 11,
    "numberSectionStyles": 4,
    "axisFormat": "%Y-%m-%d",
    "topAxis": false,
    "displayMode": "",
    "weekday": "sunday"
  },
  "journey": {
    "useMaxWidth": true,
    "diagramMarginX": 50,
    "diagramMarginY": 10,
    "leftMargin": 150,
    "maxLabelWidth": 360,
    "width": 150,
    "height": 50,
    "boxMargin": 10,
    "boxTextMargin": 5,
    "noteMargin": 10,
    "messageMargin": 35,
    "messageAlign": "center",
    "bottomMarginAdj": 1,
    "rightAngles": false,
    "taskFontSize": 14,
    "taskFontFamily": '"Open Sans", sans-serif',
    "taskMargin": 50,
    "activationWidth": 10,
    "textPlacement": "fo",
    "actorColours": [
      "#8FBC8F",
      "#7CFC00",
      "#00FFFF",
      "#20B2AA",
      "#B0E0E6",
      "#FFFFE0"
    ],
    "sectionFills": [
      "#191970",
      "#8B008B",
      "#4B0082",
      "#2F4F4F",
      "#800000",
      "#8B4513",
      "#00008B"
    ],
    "sectionColours": [
      "#fff"
    ],
    "titleColor": "",
    "titleFontFamily": '"trebuchet ms", verdana, arial, sans-serif',
    "titleFontSize": "4ex"
  },
  "class": {
    "useMaxWidth": true,
    "titleTopMargin": 25,
    "arrowMarkerAbsolute": false,
    "dividerMargin": 10,
    "padding": 5,
    "textHeight": 10,
    "defaultRenderer": "dagre-wrapper",
    "htmlLabels": false,
    "hideEmptyMembersBox": false
  },
  "state": {
    "useMaxWidth": true,
    "titleTopMargin": 25,
    "dividerMargin": 10,
    "sizeUnit": 5,
    "padding": 8,
    "textHeight": 10,
    "titleShift": -15,
    "noteMargin": 10,
    "forkWidth": 70,
    "forkHeight": 7,
    "miniPadding": 2,
    "fontSizeFactor": 5.02,
    "fontSize": 24,
    "labelHeight": 16,
    "edgeLengthFactor": "20",
    "compositTitleSize": 35,
    "radius": 5,
    "defaultRenderer": "dagre-wrapper"
  },
  "er": {
    "useMaxWidth": true,
    "titleTopMargin": 25,
    "diagramPadding": 20,
    "layoutDirection": "TB",
    "minEntityWidth": 100,
    "minEntityHeight": 75,
    "entityPadding": 15,
    "nodeSpacing": 140,
    "rankSpacing": 80,
    "stroke": "gray",
    "fill": "honeydew",
    "fontSize": 12
  },
  "pie": {
    "useMaxWidth": true,
    "textPosition": 0.75
  },
  "quadrantChart": {
    "useMaxWidth": true,
    "chartWidth": 500,
    "chartHeight": 500,
    "titleFontSize": 20,
    "titlePadding": 10,
    "quadrantPadding": 5,
    "xAxisLabelPadding": 5,
    "yAxisLabelPadding": 5,
    "xAxisLabelFontSize": 16,
    "yAxisLabelFontSize": 16,
    "quadrantLabelFontSize": 16,
    "quadrantTextTopPadding": 5,
    "pointTextPadding": 5,
    "pointLabelFontSize": 12,
    "pointRadius": 5,
    "xAxisPosition": "top",
    "yAxisPosition": "left",
    "quadrantInternalBorderStrokeWidth": 1,
    "quadrantExternalBorderStrokeWidth": 2
  },
  "xyChart": {
    "useMaxWidth": true,
    "width": 700,
    "height": 500,
    "titleFontSize": 20,
    "titlePadding": 10,
    "showDataLabel": false,
    "showTitle": true,
    "xAxis": {
      "$ref": "#/$defs/XYChartAxisConfig",
      "showLabel": true,
      "labelFontSize": 14,
      "labelPadding": 5,
      "showTitle": true,
      "titleFontSize": 16,
      "titlePadding": 5,
      "showTick": true,
      "tickLength": 5,
      "tickWidth": 2,
      "showAxisLine": true,
      "axisLineWidth": 2
    },
    "yAxis": {
      "$ref": "#/$defs/XYChartAxisConfig",
      "showLabel": true,
      "labelFontSize": 14,
      "labelPadding": 5,
      "showTitle": true,
      "titleFontSize": 16,
      "titlePadding": 5,
      "showTick": true,
      "tickLength": 5,
      "tickWidth": 2,
      "showAxisLine": true,
      "axisLineWidth": 2
    },
    "chartOrientation": "vertical",
    "plotReservedSpacePercent": 50
  },
  "requirement": {
    "useMaxWidth": true,
    "rect_fill": "#f9f9f9",
    "text_color": "#333",
    "rect_border_size": "0.5px",
    "rect_border_color": "#bbb",
    "rect_min_width": 200,
    "rect_min_height": 200,
    "fontSize": 14,
    "rect_padding": 10,
    "line_height": 20
  },
  "mindmap": {
    "useMaxWidth": true,
    "padding": 10,
    "maxNodeWidth": 200,
    "layoutAlgorithm": "cose-bilkent"
  },
  "ishikawa": {
    "useMaxWidth": true,
    "diagramPadding": 20
  },
  "kanban": {
    "useMaxWidth": true,
    "padding": 8,
    "sectionWidth": 200,
    "ticketBaseUrl": ""
  },
  "timeline": {
    "useMaxWidth": true,
    "diagramMarginX": 50,
    "diagramMarginY": 10,
    "leftMargin": 150,
    "width": 150,
    "height": 50,
    "boxMargin": 10,
    "boxTextMargin": 5,
    "noteMargin": 10,
    "messageMargin": 35,
    "messageAlign": "center",
    "bottomMarginAdj": 1,
    "rightAngles": false,
    "taskFontSize": 14,
    "taskFontFamily": '"Open Sans", sans-serif',
    "taskMargin": 50,
    "activationWidth": 10,
    "textPlacement": "fo",
    "actorColours": [
      "#8FBC8F",
      "#7CFC00",
      "#00FFFF",
      "#20B2AA",
      "#B0E0E6",
      "#FFFFE0"
    ],
    "sectionFills": [
      "#191970",
      "#8B008B",
      "#4B0082",
      "#2F4F4F",
      "#800000",
      "#8B4513",
      "#00008B"
    ],
    "sectionColours": [
      "#fff"
    ],
    "disableMulticolor": false
  },
  "gitGraph": {
    "useMaxWidth": true,
    "titleTopMargin": 25,
    "diagramPadding": 8,
    "nodeLabel": {
      "width": 75,
      "height": 100,
      "x": -25,
      "y": 0
    },
    "mainBranchName": "main",
    "mainBranchOrder": 0,
    "showCommitLabel": true,
    "showBranches": true,
    "rotateCommitLabel": true,
    "parallelCommits": false,
    "arrowMarkerAbsolute": false
  },
  "c4": {
    "useMaxWidth": true,
    "diagramMarginX": 50,
    "diagramMarginY": 10,
    "c4ShapeMargin": 50,
    "c4ShapePadding": 20,
    "width": 216,
    "height": 60,
    "boxMargin": 10,
    "c4ShapeInRow": 4,
    "nextLinePaddingX": 0,
    "c4BoundaryInRow": 2,
    "personFontSize": 14,
    "personFontFamily": '"Open Sans", sans-serif',
    "personFontWeight": "normal",
    "external_personFontSize": 14,
    "external_personFontFamily": '"Open Sans", sans-serif',
    "external_personFontWeight": "normal",
    "systemFontSize": 14,
    "systemFontFamily": '"Open Sans", sans-serif',
    "systemFontWeight": "normal",
    "external_systemFontSize": 14,
    "external_systemFontFamily": '"Open Sans", sans-serif',
    "external_systemFontWeight": "normal",
    "system_dbFontSize": 14,
    "system_dbFontFamily": '"Open Sans", sans-serif',
    "system_dbFontWeight": "normal",
    "external_system_dbFontSize": 14,
    "external_system_dbFontFamily": '"Open Sans", sans-serif',
    "external_system_dbFontWeight": "normal",
    "system_queueFontSize": 14,
    "system_queueFontFamily": '"Open Sans", sans-serif',
    "system_queueFontWeight": "normal",
    "external_system_queueFontSize": 14,
    "external_system_queueFontFamily": '"Open Sans", sans-serif',
    "external_system_queueFontWeight": "normal",
    "boundaryFontSize": 14,
    "boundaryFontFamily": '"Open Sans", sans-serif',
    "boundaryFontWeight": "normal",
    "messageFontSize": 12,
    "messageFontFamily": '"Open Sans", sans-serif',
    "messageFontWeight": "normal",
    "containerFontSize": 14,
    "containerFontFamily": '"Open Sans", sans-serif',
    "containerFontWeight": "normal",
    "external_containerFontSize": 14,
    "external_containerFontFamily": '"Open Sans", sans-serif',
    "external_containerFontWeight": "normal",
    "container_dbFontSize": 14,
    "container_dbFontFamily": '"Open Sans", sans-serif',
    "container_dbFontWeight": "normal",
    "external_container_dbFontSize": 14,
    "external_container_dbFontFamily": '"Open Sans", sans-serif',
    "external_container_dbFontWeight": "normal",
    "container_queueFontSize": 14,
    "container_queueFontFamily": '"Open Sans", sans-serif',
    "container_queueFontWeight": "normal",
    "external_container_queueFontSize": 14,
    "external_container_queueFontFamily": '"Open Sans", sans-serif',
    "external_container_queueFontWeight": "normal",
    "componentFontSize": 14,
    "componentFontFamily": '"Open Sans", sans-serif',
    "componentFontWeight": "normal",
    "external_componentFontSize": 14,
    "external_componentFontFamily": '"Open Sans", sans-serif',
    "external_componentFontWeight": "normal",
    "component_dbFontSize": 14,
    "component_dbFontFamily": '"Open Sans", sans-serif',
    "component_dbFontWeight": "normal",
    "external_component_dbFontSize": 14,
    "external_component_dbFontFamily": '"Open Sans", sans-serif',
    "external_component_dbFontWeight": "normal",
    "component_queueFontSize": 14,
    "component_queueFontFamily": '"Open Sans", sans-serif',
    "component_queueFontWeight": "normal",
    "external_component_queueFontSize": 14,
    "external_component_queueFontFamily": '"Open Sans", sans-serif',
    "external_component_queueFontWeight": "normal",
    "wrap": true,
    "wrapPadding": 10,
    "person_bg_color": "#08427B",
    "person_border_color": "#073B6F",
    "external_person_bg_color": "#686868",
    "external_person_border_color": "#8A8A8A",
    "system_bg_color": "#1168BD",
    "system_border_color": "#3C7FC0",
    "system_db_bg_color": "#1168BD",
    "system_db_border_color": "#3C7FC0",
    "system_queue_bg_color": "#1168BD",
    "system_queue_border_color": "#3C7FC0",
    "external_system_bg_color": "#999999",
    "external_system_border_color": "#8A8A8A",
    "external_system_db_bg_color": "#999999",
    "external_system_db_border_color": "#8A8A8A",
    "external_system_queue_bg_color": "#999999",
    "external_system_queue_border_color": "#8A8A8A",
    "container_bg_color": "#438DD5",
    "container_border_color": "#3C7FC0",
    "container_db_bg_color": "#438DD5",
    "container_db_border_color": "#3C7FC0",
    "container_queue_bg_color": "#438DD5",
    "container_queue_border_color": "#3C7FC0",
    "external_container_bg_color": "#B3B3B3",
    "external_container_border_color": "#A6A6A6",
    "external_container_db_bg_color": "#B3B3B3",
    "external_container_db_border_color": "#A6A6A6",
    "external_container_queue_bg_color": "#B3B3B3",
    "external_container_queue_border_color": "#A6A6A6",
    "component_bg_color": "#85BBF0",
    "component_border_color": "#78A8D8",
    "component_db_bg_color": "#85BBF0",
    "component_db_border_color": "#78A8D8",
    "component_queue_bg_color": "#85BBF0",
    "component_queue_border_color": "#78A8D8",
    "external_component_bg_color": "#CCCCCC",
    "external_component_border_color": "#BFBFBF",
    "external_component_db_bg_color": "#CCCCCC",
    "external_component_db_border_color": "#BFBFBF",
    "external_component_queue_bg_color": "#CCCCCC",
    "external_component_queue_border_color": "#BFBFBF"
  },
  "sankey": {
    "useMaxWidth": true,
    "width": 600,
    "height": 400,
    "linkColor": "gradient",
    "nodeAlignment": "justify",
    "showValues": true,
    "prefix": "",
    "suffix": ""
  },
  "block": {
    "useMaxWidth": true,
    "padding": 8
  },
  "packet": {
    "useMaxWidth": true,
    "rowHeight": 32,
    "bitWidth": 32,
    "bitsPerRow": 32,
    "showBits": true,
    "paddingX": 5,
    "paddingY": 5
  },
  "architecture": {
    "useMaxWidth": true,
    "padding": 40,
    "iconSize": 80,
    "fontSize": 16
  },
  "radar": {
    "useMaxWidth": true,
    "width": 600,
    "height": 600,
    "marginTop": 50,
    "marginRight": 50,
    "marginBottom": 50,
    "marginLeft": 50,
    "axisScaleFactor": 1,
    "axisLabelFactor": 1.05,
    "curveTension": 0.17
  },
  "venn": {
    "useMaxWidth": true,
    "width": 800,
    "height": 450,
    "padding": 8,
    "useDebugLayout": false
  },
  "theme": "default",
  "look": "classic",
  "handDrawnSeed": 0,
  "layout": "dagre",
  "maxTextSize": 5e4,
  "maxEdges": 500,
  "darkMode": false,
  "fontFamily": '"trebuchet ms", verdana, arial, sans-serif;',
  "logLevel": 5,
  "securityLevel": "strict",
  "startOnLoad": true,
  "arrowMarkerAbsolute": false,
  "secure": [
    "secure",
    "securityLevel",
    "startOnLoad",
    "maxTextSize",
    "suppressErrorRendering",
    "maxEdges"
  ],
  "legacyMathML": false,
  "forceLegacyMathML": false,
  "deterministicIds": false,
  "fontSize": 16,
  "markdownAutoWrap": true,
  "suppressErrorRendering": false
};
var config = {
  ...config_schema_default,
  // Set, even though they're `undefined` so that `configKeys` finds these keys
  // TODO: Should we replace these with `null` so that they can go in the JSON Schema?
  deterministicIDSeed: void 0,
  elk: {
    // mergeEdges is needed here to be considered
    mergeEdges: false,
    nodePlacementStrategy: "BRANDES_KOEPF",
    forceNodeModelOrder: false,
    considerModelOrder: "NODES_AND_EDGES"
  },
  themeCSS: void 0,
  // add non-JSON default config values
  themeVariables: themes_default.default.getThemeVariables(),
  sequence: {
    ...config_schema_default.sequence,
    messageFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.messageFontFamily,
        fontSize: this.messageFontSize,
        fontWeight: this.messageFontWeight
      };
    }, "messageFont"),
    noteFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.noteFontFamily,
        fontSize: this.noteFontSize,
        fontWeight: this.noteFontWeight
      };
    }, "noteFont"),
    actorFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.actorFontFamily,
        fontSize: this.actorFontSize,
        fontWeight: this.actorFontWeight
      };
    }, "actorFont")
  },
  class: {
    hideEmptyMembersBox: false
  },
  gantt: {
    ...config_schema_default.gantt,
    tickInterval: void 0,
    useWidth: void 0
    // can probably be removed since `configKeys` already includes this
  },
  c4: {
    ...config_schema_default.c4,
    useWidth: void 0,
    personFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.personFontFamily,
        fontSize: this.personFontSize,
        fontWeight: this.personFontWeight
      };
    }, "personFont"),
    flowchart: {
      ...config_schema_default.flowchart,
      inheritDir: false
      // default to legacy behavior
    },
    external_personFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_personFontFamily,
        fontSize: this.external_personFontSize,
        fontWeight: this.external_personFontWeight
      };
    }, "external_personFont"),
    systemFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.systemFontFamily,
        fontSize: this.systemFontSize,
        fontWeight: this.systemFontWeight
      };
    }, "systemFont"),
    external_systemFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_systemFontFamily,
        fontSize: this.external_systemFontSize,
        fontWeight: this.external_systemFontWeight
      };
    }, "external_systemFont"),
    system_dbFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.system_dbFontFamily,
        fontSize: this.system_dbFontSize,
        fontWeight: this.system_dbFontWeight
      };
    }, "system_dbFont"),
    external_system_dbFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_system_dbFontFamily,
        fontSize: this.external_system_dbFontSize,
        fontWeight: this.external_system_dbFontWeight
      };
    }, "external_system_dbFont"),
    system_queueFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.system_queueFontFamily,
        fontSize: this.system_queueFontSize,
        fontWeight: this.system_queueFontWeight
      };
    }, "system_queueFont"),
    external_system_queueFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_system_queueFontFamily,
        fontSize: this.external_system_queueFontSize,
        fontWeight: this.external_system_queueFontWeight
      };
    }, "external_system_queueFont"),
    containerFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.containerFontFamily,
        fontSize: this.containerFontSize,
        fontWeight: this.containerFontWeight
      };
    }, "containerFont"),
    external_containerFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_containerFontFamily,
        fontSize: this.external_containerFontSize,
        fontWeight: this.external_containerFontWeight
      };
    }, "external_containerFont"),
    container_dbFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.container_dbFontFamily,
        fontSize: this.container_dbFontSize,
        fontWeight: this.container_dbFontWeight
      };
    }, "container_dbFont"),
    external_container_dbFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_container_dbFontFamily,
        fontSize: this.external_container_dbFontSize,
        fontWeight: this.external_container_dbFontWeight
      };
    }, "external_container_dbFont"),
    container_queueFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.container_queueFontFamily,
        fontSize: this.container_queueFontSize,
        fontWeight: this.container_queueFontWeight
      };
    }, "container_queueFont"),
    external_container_queueFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_container_queueFontFamily,
        fontSize: this.external_container_queueFontSize,
        fontWeight: this.external_container_queueFontWeight
      };
    }, "external_container_queueFont"),
    componentFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.componentFontFamily,
        fontSize: this.componentFontSize,
        fontWeight: this.componentFontWeight
      };
    }, "componentFont"),
    external_componentFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_componentFontFamily,
        fontSize: this.external_componentFontSize,
        fontWeight: this.external_componentFontWeight
      };
    }, "external_componentFont"),
    component_dbFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.component_dbFontFamily,
        fontSize: this.component_dbFontSize,
        fontWeight: this.component_dbFontWeight
      };
    }, "component_dbFont"),
    external_component_dbFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_component_dbFontFamily,
        fontSize: this.external_component_dbFontSize,
        fontWeight: this.external_component_dbFontWeight
      };
    }, "external_component_dbFont"),
    component_queueFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.component_queueFontFamily,
        fontSize: this.component_queueFontSize,
        fontWeight: this.component_queueFontWeight
      };
    }, "component_queueFont"),
    external_component_queueFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.external_component_queueFontFamily,
        fontSize: this.external_component_queueFontSize,
        fontWeight: this.external_component_queueFontWeight
      };
    }, "external_component_queueFont"),
    boundaryFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.boundaryFontFamily,
        fontSize: this.boundaryFontSize,
        fontWeight: this.boundaryFontWeight
      };
    }, "boundaryFont"),
    messageFont: /* @__PURE__ */ __name(function() {
      return {
        fontFamily: this.messageFontFamily,
        fontSize: this.messageFontSize,
        fontWeight: this.messageFontWeight
      };
    }, "messageFont")
  },
  pie: {
    ...config_schema_default.pie,
    useWidth: 984
  },
  xyChart: {
    ...config_schema_default.xyChart,
    useWidth: void 0
  },
  requirement: {
    ...config_schema_default.requirement,
    useWidth: void 0
  },
  packet: {
    ...config_schema_default.packet
  },
  radar: {
    ...config_schema_default.radar
  },
  ishikawa: {
    ...config_schema_default.ishikawa
  },
  treemap: {
    useMaxWidth: true,
    padding: 10,
    diagramPadding: 8,
    showValues: true,
    nodeWidth: 100,
    nodeHeight: 40,
    borderWidth: 1,
    valueFontSize: 12,
    labelFontSize: 14,
    valueFormat: ","
  },
  venn: {
    ...config_schema_default.venn
  }
};
var keyify = /* @__PURE__ */ __name((obj, prefix = "") => Object.keys(obj).reduce((res, el) => {
  if (Array.isArray(obj[el])) {
    return res;
  } else if (typeof obj[el] === "object" && obj[el] !== null) {
    return [...res, prefix + el, ...keyify(obj[el], "")];
  }
  return [...res, prefix + el];
}, []), "keyify");
var configKeys = new Set(keyify(config, ""));
var defaultConfig_default = config;
var sanitizeDirective = /* @__PURE__ */ __name((args) => {
  log.debug("sanitizeDirective called with", args);
  if (typeof args !== "object" || args == null) {
    return;
  }
  if (Array.isArray(args)) {
    args.forEach((arg) => sanitizeDirective(arg));
    return;
  }
  for (const key of Object.keys(args)) {
    log.debug("Checking key", key);
    if (key.startsWith("__") || key.includes("proto") || key.includes("constr") || !configKeys.has(key) || args[key] == null) {
      log.debug("sanitize deleting key: ", key);
      delete args[key];
      continue;
    }
    if (typeof args[key] === "object") {
      log.debug("sanitizing object", key);
      sanitizeDirective(args[key]);
      continue;
    }
    const cssMatchers = ["themeCSS", "fontFamily", "altFontFamily"];
    for (const cssKey of cssMatchers) {
      if (key.includes(cssKey)) {
        log.debug("sanitizing css option", key);
        args[key] = sanitizeCss(args[key]);
      }
    }
  }
  if (args.themeVariables) {
    for (const k of Object.keys(args.themeVariables)) {
      const val = args.themeVariables[k];
      if (val?.match && !val.match(/^[\d "#%(),.;A-Za-z]+$/)) {
        args.themeVariables[k] = "";
      }
    }
  }
  log.debug("After sanitization", args);
}, "sanitizeDirective");
var sanitizeCss = /* @__PURE__ */ __name((str) => {
  let startCnt = 0;
  let endCnt = 0;
  for (const element of str) {
    if (startCnt < endCnt) {
      return "{ /* ERROR: Unbalanced CSS */ }";
    }
    if (element === "{") {
      startCnt++;
    } else if (element === "}") {
      endCnt++;
    }
  }
  if (startCnt !== endCnt) {
    return "{ /* ERROR: Unbalanced CSS */ }";
  }
  return str;
}, "sanitizeCss");
var defaultConfig = Object.freeze(defaultConfig_default);
var evaluate = /* @__PURE__ */ __name((val) => val === false || ["false", "null", "0"].includes(String(val).trim().toLowerCase()) ? false : true, "evaluate");
var siteConfig = assignWithDepth_default({}, defaultConfig);
var configFromInitialize;
var directives = [];
var currentConfig = assignWithDepth_default({}, defaultConfig);
var updateCurrentConfig = /* @__PURE__ */ __name((siteCfg, _directives) => {
  let cfg = assignWithDepth_default({}, siteCfg);
  let sumOfDirectives = {};
  for (const d of _directives) {
    sanitize(d);
    sumOfDirectives = assignWithDepth_default(sumOfDirectives, d);
  }
  cfg = assignWithDepth_default(cfg, sumOfDirectives);
  if (sumOfDirectives.theme && sumOfDirectives.theme in themes_default) {
    const tmpConfigFromInitialize = assignWithDepth_default({}, configFromInitialize);
    const themeVariables = assignWithDepth_default(
      tmpConfigFromInitialize.themeVariables || {},
      sumOfDirectives.themeVariables
    );
    if (cfg.theme && cfg.theme in themes_default) {
      cfg.themeVariables = themes_default[cfg.theme].getThemeVariables(themeVariables);
    }
  }
  currentConfig = cfg;
  checkConfig(currentConfig);
  return currentConfig;
}, "updateCurrentConfig");
var setSiteConfig = /* @__PURE__ */ __name((conf) => {
  siteConfig = assignWithDepth_default({}, defaultConfig);
  siteConfig = assignWithDepth_default(siteConfig, conf);
  if (conf.theme && themes_default[conf.theme]) {
    siteConfig.themeVariables = themes_default[conf.theme].getThemeVariables(conf.themeVariables);
  }
  updateCurrentConfig(siteConfig, directives);
  return siteConfig;
}, "setSiteConfig");
var saveConfigFromInitialize = /* @__PURE__ */ __name((conf) => {
  configFromInitialize = assignWithDepth_default({}, conf);
}, "saveConfigFromInitialize");
var updateSiteConfig = /* @__PURE__ */ __name((conf) => {
  siteConfig = assignWithDepth_default(siteConfig, conf);
  updateCurrentConfig(siteConfig, directives);
  return siteConfig;
}, "updateSiteConfig");
var getSiteConfig = /* @__PURE__ */ __name(() => {
  return assignWithDepth_default({}, siteConfig);
}, "getSiteConfig");
var setConfig = /* @__PURE__ */ __name((conf) => {
  checkConfig(conf);
  assignWithDepth_default(currentConfig, conf);
  return getConfig();
}, "setConfig");
var getConfig = /* @__PURE__ */ __name(() => {
  return assignWithDepth_default({}, currentConfig);
}, "getConfig");
var sanitize = /* @__PURE__ */ __name((options) => {
  if (!options) {
    return;
  }
  ["secure", ...siteConfig.secure ?? []].forEach((key) => {
    if (Object.hasOwn(options, key)) {
      log.debug(`Denied attempt to modify a secure key ${key}`, options[key]);
      delete options[key];
    }
  });
  Object.keys(options).forEach((key) => {
    if (key.startsWith("__")) {
      delete options[key];
    }
  });
  Object.keys(options).forEach((key) => {
    if (typeof options[key] === "string" && (options[key].includes("<") || options[key].includes(">") || options[key].includes("url(data:"))) {
      delete options[key];
    }
    if (typeof options[key] === "object") {
      sanitize(options[key]);
    }
  });
}, "sanitize");
var addDirective = /* @__PURE__ */ __name((directive) => {
  sanitizeDirective(directive);
  if (directive.fontFamily && !directive.themeVariables?.fontFamily) {
    directive.themeVariables = {
      ...directive.themeVariables,
      fontFamily: directive.fontFamily
    };
  }
  directives.push(directive);
  updateCurrentConfig(siteConfig, directives);
}, "addDirective");
var reset = /* @__PURE__ */ __name((config2 = siteConfig) => {
  directives = [];
  updateCurrentConfig(config2, directives);
}, "reset");
var ConfigWarning = {
  LAZY_LOAD_DEPRECATED: "The configuration options lazyLoadedDiagrams and loadExternalDiagramsAtStartup are deprecated. Please use registerExternalDiagrams instead.",
  FLOWCHART_HTML_LABELS_DEPRECATED: "flowchart.htmlLabels is deprecated. Please use global htmlLabels instead."
};
var issuedWarnings = {};
var issueWarning = /* @__PURE__ */ __name((warning) => {
  if (issuedWarnings[warning]) {
    return;
  }
  log.warn(ConfigWarning[warning]);
  issuedWarnings[warning] = true;
}, "issueWarning");
var checkConfig = /* @__PURE__ */ __name((config2) => {
  if (!config2) {
    return;
  }
  if (config2.lazyLoadedDiagrams || config2.loadExternalDiagramsAtStartup) {
    issueWarning("LAZY_LOAD_DEPRECATED");
  }
}, "checkConfig");
var getUserDefinedConfig = /* @__PURE__ */ __name(() => {
  let userConfig = {};
  if (configFromInitialize) {
    userConfig = assignWithDepth_default(userConfig, configFromInitialize);
  }
  for (const d of directives) {
    userConfig = assignWithDepth_default(userConfig, d);
  }
  return userConfig;
}, "getUserDefinedConfig");
var getEffectiveHtmlLabels = /* @__PURE__ */ __name((config2) => {
  if (config2.flowchart?.htmlLabels != void 0) {
    issueWarning("FLOWCHART_HTML_LABELS_DEPRECATED");
  }
  return evaluate(config2.htmlLabels ?? config2.flowchart?.htmlLabels ?? true);
}, "getEffectiveHtmlLabels");
var lineBreakRegex = /<br\s*\/?>/gi;
var getRows = /* @__PURE__ */ __name((s) => {
  if (!s) {
    return [""];
  }
  const str = breakToPlaceholder(s).replace(/\\n/g, "#br#");
  return str.split("#br#");
}, "getRows");
var setupDompurifyHooksIfNotSetup = /* @__PURE__ */ (() => {
  let setup = false;
  return () => {
    if (!setup) {
      setupDompurifyHooks();
      setup = true;
    }
  };
})();
function setupDompurifyHooks() {
  const TEMPORARY_ATTRIBUTE = "data-temp-href-target";
  purify.addHook("beforeSanitizeAttributes", (node) => {
    if (node.tagName === "A" && node.hasAttribute("target")) {
      node.setAttribute(TEMPORARY_ATTRIBUTE, node.getAttribute("target") ?? "");
    }
  });
  purify.addHook("afterSanitizeAttributes", (node) => {
    if (node.tagName === "A" && node.hasAttribute(TEMPORARY_ATTRIBUTE)) {
      node.setAttribute("target", node.getAttribute(TEMPORARY_ATTRIBUTE) ?? "");
      node.removeAttribute(TEMPORARY_ATTRIBUTE);
      if (node.getAttribute("target") === "_blank") {
        node.setAttribute("rel", "noopener");
      }
    }
  });
}
__name(setupDompurifyHooks, "setupDompurifyHooks");
var removeScript = /* @__PURE__ */ __name((txt) => {
  setupDompurifyHooksIfNotSetup();
  const sanitizedText = purify.sanitize(txt);
  return sanitizedText;
}, "removeScript");
var sanitizeMore = /* @__PURE__ */ __name((text, config2) => {
  if (getEffectiveHtmlLabels(config2)) {
    const level = config2.securityLevel;
    if (level === "antiscript" || level === "strict" || level === "sandbox") {
      text = removeScript(text);
    } else if (level !== "loose") {
      text = breakToPlaceholder(text);
      text = text.replace(/</g, "&lt;").replace(/>/g, "&gt;");
      text = text.replace(/=/g, "&equals;");
      text = placeholderToBreak(text);
    }
  }
  return text;
}, "sanitizeMore");
var sanitizeText = /* @__PURE__ */ __name((text, config2) => {
  if (!text) {
    return text;
  }
  if (config2.dompurifyConfig) {
    text = purify.sanitize(sanitizeMore(text, config2), config2.dompurifyConfig).toString();
  } else {
    text = purify.sanitize(sanitizeMore(text, config2), {
      FORBID_TAGS: ["style"]
    }).toString();
  }
  return text;
}, "sanitizeText");
var sanitizeTextOrArray = /* @__PURE__ */ __name((a, config2) => {
  if (typeof a === "string") {
    return sanitizeText(a, config2);
  }
  return a.flat().map((x) => sanitizeText(x, config2));
}, "sanitizeTextOrArray");
var hasBreaks = /* @__PURE__ */ __name((text) => {
  return lineBreakRegex.test(text);
}, "hasBreaks");
var splitBreaks = /* @__PURE__ */ __name((text) => {
  return text.split(lineBreakRegex);
}, "splitBreaks");
var placeholderToBreak = /* @__PURE__ */ __name((s) => {
  return s.replace(/#br#/g, "<br/>");
}, "placeholderToBreak");
var breakToPlaceholder = /* @__PURE__ */ __name((s) => {
  return s.replace(lineBreakRegex, "#br#");
}, "breakToPlaceholder");
var getUrl = /* @__PURE__ */ __name((useAbsolute) => {
  let url = "";
  if (useAbsolute) {
    url = window.location.protocol + "//" + window.location.host + window.location.pathname + window.location.search;
    url = CSS.escape(url);
  }
  return url;
}, "getUrl");
var getMax = /* @__PURE__ */ __name(function(...values) {
  const newValues = values.filter((value) => {
    return !isNaN(value);
  });
  return Math.max(...newValues);
}, "getMax");
var getMin = /* @__PURE__ */ __name(function(...values) {
  const newValues = values.filter((value) => {
    return !isNaN(value);
  });
  return Math.min(...newValues);
}, "getMin");
var parseGenericTypes = /* @__PURE__ */ __name(function(input) {
  const inputSets = input.split(/(,)/);
  const output = [];
  for (let i = 0; i < inputSets.length; i++) {
    let thisSet = inputSets[i];
    if (thisSet === "," && i > 0 && i + 1 < inputSets.length) {
      const previousSet = inputSets[i - 1];
      const nextSet = inputSets[i + 1];
      if (shouldCombineSets(previousSet, nextSet)) {
        thisSet = previousSet + "," + nextSet;
        i++;
        output.pop();
      }
    }
    output.push(processSet(thisSet));
  }
  return output.join("");
}, "parseGenericTypes");
var countOccurrence = /* @__PURE__ */ __name((string, substring) => {
  return Math.max(0, string.split(substring).length - 1);
}, "countOccurrence");
var shouldCombineSets = /* @__PURE__ */ __name((previousSet, nextSet) => {
  const prevCount = countOccurrence(previousSet, "~");
  const nextCount = countOccurrence(nextSet, "~");
  return prevCount === 1 && nextCount === 1;
}, "shouldCombineSets");
var processSet = /* @__PURE__ */ __name((input) => {
  const tildeCount = countOccurrence(input, "~");
  let hasStartingTilde = false;
  if (tildeCount <= 1) {
    return input;
  }
  if (tildeCount % 2 !== 0 && input.startsWith("~")) {
    input = input.substring(1);
    hasStartingTilde = true;
  }
  const chars = [...input];
  let first = chars.indexOf("~");
  let last = chars.lastIndexOf("~");
  while (first !== -1 && last !== -1 && first !== last) {
    chars[first] = "<";
    chars[last] = ">";
    first = chars.indexOf("~");
    last = chars.lastIndexOf("~");
  }
  if (hasStartingTilde) {
    chars.unshift("~");
  }
  return chars.join("");
}, "processSet");
var isMathMLSupported = /* @__PURE__ */ __name(() => window.MathMLElement !== void 0, "isMathMLSupported");
var katexRegex = /\$\$(.*)\$\$/g;
var hasKatex = /* @__PURE__ */ __name((text) => (text.match(katexRegex)?.length ?? 0) > 0, "hasKatex");
var calculateMathMLDimensions = /* @__PURE__ */ __name(async (text, config2) => {
  const divElem = document.createElement("div");
  divElem.innerHTML = await renderKatexSanitized(text, config2);
  divElem.id = "katex-temp";
  divElem.style.visibility = "hidden";
  divElem.style.position = "absolute";
  divElem.style.top = "0";
  const body = document.querySelector("body");
  body?.insertAdjacentElement("beforeend", divElem);
  const dim = { width: divElem.clientWidth, height: divElem.clientHeight };
  divElem.remove();
  return dim;
}, "calculateMathMLDimensions");
var renderKatexUnsanitized = /* @__PURE__ */ __name(async (text, config2) => {
  if (!hasKatex(text)) {
    return text;
  }
  if (!(isMathMLSupported() || config2.legacyMathML || config2.forceLegacyMathML)) {
    return text.replace(katexRegex, "MathML is unsupported in this environment.");
  }
  if (true) {
    const { default: katex } = await import("./katex-FGDLI6ED.js");
    const outputMode = config2.forceLegacyMathML || !isMathMLSupported() && config2.legacyMathML ? "htmlAndMathml" : "mathml";
    return text.split(lineBreakRegex).map(
      (line) => hasKatex(line) ? `<div style="display: flex; align-items: center; justify-content: center; white-space: nowrap;">${line}</div>` : `<div>${line}</div>`
    ).join("").replace(
      katexRegex,
      (_, c) => katex.renderToString(c, {
        throwOnError: true,
        displayMode: true,
        output: outputMode
      }).replace(/\n/g, " ").replace(/<annotation.*<\/annotation>/g, "")
    );
  }
  return text.replace(
    katexRegex,
    "Katex is not supported in @mermaid-js/tiny. Please use the full mermaid library."
  );
}, "renderKatexUnsanitized");
var renderKatexSanitized = /* @__PURE__ */ __name(async (text, config2) => {
  return sanitizeText(await renderKatexUnsanitized(text, config2), config2);
}, "renderKatexSanitized");
var common_default = {
  getRows,
  sanitizeText,
  sanitizeTextOrArray,
  hasBreaks,
  splitBreaks,
  lineBreakRegex,
  removeScript,
  getUrl,
  evaluate,
  getMax,
  getMin
};
var d3Attrs = /* @__PURE__ */ __name(function(d3Elem, attrs) {
  for (let attr of attrs) {
    d3Elem.attr(attr[0], attr[1]);
  }
}, "d3Attrs");
var calculateSvgSizeAttrs = /* @__PURE__ */ __name(function(height, width, useMaxWidth) {
  let attrs = /* @__PURE__ */ new Map();
  if (useMaxWidth) {
    attrs.set("width", "100%");
    attrs.set("style", `max-width: ${width}px;`);
  } else {
    attrs.set("height", height);
    attrs.set("width", width);
  }
  return attrs;
}, "calculateSvgSizeAttrs");
var configureSvgSize = /* @__PURE__ */ __name(function(svgElem, height, width, useMaxWidth) {
  const attrs = calculateSvgSizeAttrs(height, width, useMaxWidth);
  d3Attrs(svgElem, attrs);
}, "configureSvgSize");
var setupGraphViewbox = /* @__PURE__ */ __name(function(graph, svgElem, padding, useMaxWidth) {
  const svgBounds = svgElem.node().getBBox();
  const sWidth = svgBounds.width;
  const sHeight = svgBounds.height;
  log.info(`SVG bounds: ${sWidth}x${sHeight}`, svgBounds);
  let width = 0;
  let height = 0;
  log.info(`Graph bounds: ${width}x${height}`, graph);
  width = sWidth + padding * 2;
  height = sHeight + padding * 2;
  log.info(`Calculated bounds: ${width}x${height}`);
  configureSvgSize(svgElem, height, width, useMaxWidth);
  const vBox = `${svgBounds.x - padding} ${svgBounds.y - padding} ${svgBounds.width + 2 * padding} ${svgBounds.height + 2 * padding}`;
  svgElem.attr("viewBox", vBox);
}, "setupGraphViewbox");
var themes = {};
var getStyles = /* @__PURE__ */ __name((type, userStyles, options) => {
  let diagramStyles = "";
  if (type in themes && themes[type]) {
    diagramStyles = themes[type](options);
  } else {
    log.warn(`No theme found for ${type}`);
  }
  return ` & {
    font-family: ${options.fontFamily};
    font-size: ${options.fontSize};
    fill: ${options.textColor}
  }
  @keyframes edge-animation-frame {
    from {
      stroke-dashoffset: 0;
    }
  }
  @keyframes dash {
    to {
      stroke-dashoffset: 0;
    }
  }
  & .edge-animation-slow {
    stroke-dasharray: 9,5 !important;
    stroke-dashoffset: 900;
    animation: dash 50s linear infinite;
    stroke-linecap: round;
  }
  & .edge-animation-fast {
    stroke-dasharray: 9,5 !important;
    stroke-dashoffset: 900;
    animation: dash 20s linear infinite;
    stroke-linecap: round;
  }
  /* Classes common for multiple diagrams */

  & .error-icon {
    fill: ${options.errorBkgColor};
  }
  & .error-text {
    fill: ${options.errorTextColor};
    stroke: ${options.errorTextColor};
  }

  & .edge-thickness-normal {
    stroke-width: 1px;
  }
  & .edge-thickness-thick {
    stroke-width: 3.5px
  }
  & .edge-pattern-solid {
    stroke-dasharray: 0;
  }
  & .edge-thickness-invisible {
    stroke-width: 0;
    fill: none;
  }
  & .edge-pattern-dashed{
    stroke-dasharray: 3;
  }
  .edge-pattern-dotted {
    stroke-dasharray: 2;
  }

  & .marker {
    fill: ${options.lineColor};
    stroke: ${options.lineColor};
  }
  & .marker.cross {
    stroke: ${options.lineColor};
  }

  & svg {
    font-family: ${options.fontFamily};
    font-size: ${options.fontSize};
  }
   & p {
    margin: 0
   }

  ${diagramStyles}

  ${userStyles}
`;
}, "getStyles");
var addStylesForDiagram = /* @__PURE__ */ __name((type, diagramTheme) => {
  if (diagramTheme !== void 0) {
    themes[type] = diagramTheme;
  }
}, "addStylesForDiagram");
var styles_default = getStyles;
var commonDb_exports = {};
__export(commonDb_exports, {
  clear: () => clear,
  getAccDescription: () => getAccDescription,
  getAccTitle: () => getAccTitle,
  getDiagramTitle: () => getDiagramTitle,
  setAccDescription: () => setAccDescription,
  setAccTitle: () => setAccTitle,
  setDiagramTitle: () => setDiagramTitle
});
var accTitle = "";
var diagramTitle = "";
var accDescription = "";
var sanitizeText2 = /* @__PURE__ */ __name((txt) => sanitizeText(txt, getConfig()), "sanitizeText");
var clear = /* @__PURE__ */ __name(() => {
  accTitle = "";
  accDescription = "";
  diagramTitle = "";
}, "clear");
var setAccTitle = /* @__PURE__ */ __name((txt) => {
  accTitle = sanitizeText2(txt).replace(/^\s+/g, "");
}, "setAccTitle");
var getAccTitle = /* @__PURE__ */ __name(() => accTitle, "getAccTitle");
var setAccDescription = /* @__PURE__ */ __name((txt) => {
  accDescription = sanitizeText2(txt).replace(/\n\s+/g, "\n");
}, "setAccDescription");
var getAccDescription = /* @__PURE__ */ __name(() => accDescription, "getAccDescription");
var setDiagramTitle = /* @__PURE__ */ __name((txt) => {
  diagramTitle = sanitizeText2(txt);
}, "setDiagramTitle");
var getDiagramTitle = /* @__PURE__ */ __name(() => diagramTitle, "getDiagramTitle");
var log2 = log;
var setLogLevel2 = setLogLevel;
var getConfig2 = getConfig;
var setConfig2 = setConfig;
var defaultConfig2 = defaultConfig;
var sanitizeText3 = /* @__PURE__ */ __name((text) => sanitizeText(text, getConfig2()), "sanitizeText");
var setupGraphViewbox2 = setupGraphViewbox;
var getCommonDb = /* @__PURE__ */ __name(() => {
  return commonDb_exports;
}, "getCommonDb");
var diagrams = {};
var registerDiagram = /* @__PURE__ */ __name((id, diagram, detector) => {
  if (diagrams[id]) {
    log2.warn(`Diagram with id ${id} already registered. Overwriting.`);
  }
  diagrams[id] = diagram;
  if (detector) {
    addDetector(id, detector);
  }
  addStylesForDiagram(id, diagram.styles);
  diagram.injectUtils?.(
    log2,
    setLogLevel2,
    getConfig2,
    sanitizeText3,
    setupGraphViewbox2,
    getCommonDb(),
    () => {
    }
  );
}, "registerDiagram");
var getDiagram = /* @__PURE__ */ __name((name) => {
  if (name in diagrams) {
    return diagrams[name];
  }
  throw new DiagramNotFoundError(name);
}, "getDiagram");
var _a7;
var DiagramNotFoundError = (_a7 = class extends Error {
  constructor(name) {
    super(`Diagram ${name} not found.`);
  }
}, __name(_a7, "DiagramNotFoundError"), _a7);

export {
  rgba_default,
  channel_default2 as channel_default,
  is_dark_default,
  lighten_default,
  darken_default,
  transparentize_default,
  frontMatterRegex,
  directiveRegex,
  UnknownDiagramError,
  detectors,
  detectType,
  registerLazyLoadedDiagrams,
  getDiagramLoader,
  assignWithDepth_default,
  getThemeVariables3,
  themes_default,
  defaultConfig_default,
  sanitizeDirective,
  defaultConfig,
  evaluate,
  setSiteConfig,
  saveConfigFromInitialize,
  updateSiteConfig,
  getSiteConfig,
  setConfig,
  getConfig,
  addDirective,
  reset,
  getUserDefinedConfig,
  getEffectiveHtmlLabels,
  lineBreakRegex,
  sanitizeText,
  getUrl,
  parseGenericTypes,
  hasKatex,
  calculateMathMLDimensions,
  renderKatexSanitized,
  common_default,
  configureSvgSize,
  setupGraphViewbox,
  styles_default,
  commonDb_exports,
  clear,
  setAccTitle,
  getAccTitle,
  setAccDescription,
  getAccDescription,
  setDiagramTitle,
  getDiagramTitle,
  getConfig2,
  setConfig2,
  defaultConfig2,
  sanitizeText3,
  setupGraphViewbox2,
  registerDiagram,
  getDiagram
};
