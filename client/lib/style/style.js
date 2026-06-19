/**
 * @module lib/style/style.js
 * @about
 *   Single source of truth for ALL gimbal design tokens.
 *
 *   Raw values live here as JS constants.  Everything else — CSS custom
 *   properties on :root, CodeMirror themes, canvas drawing — is derived
 *   from these exports.  Nothing is duplicated.
 *
 *   Usage:
 *     // host page (call once, before widgets mount)
 *     import { injectTheme } from '../style/style.js';
 *     injectTheme();
 *
 *     // widget or theme JS
 *     import { C, hue, alpha, lighten, darken, dim } from '../style/style.js';
 */

/* ══════════════════════════════════════════════════════════
   COLOR UTILITIES
   All work on hex strings (#rrggbb or #rgb).
   Return hex strings unless noted.
   ══════════════════════════════════════════════════════════ */

function _hexToRgb(hex) {
  hex = hex.replace(/^#/, '');
  if (hex.length === 3) hex = hex.split('').map(c => c+c).join('');
  const n = parseInt(hex, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function _rgbToHex(r, g, b) {
  return '#' + [r, g, b].map(v =>
    Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, '0')
  ).join('');
}

// RGB → HSL  (h: 0–360, s: 0–1, l: 0–1)
function _rgbToHsl(r, g, b) {
  r /= 255; g /= 255; b /= 255;
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  let h, s;
  const l = (max + min) / 2;
  if (max === min) {
    h = s = 0;
  } else {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r: h = ((g - b) / d + (g < b ? 6 : 0)) / 6; break;
      case g: h = ((b - r) / d + 2) / 6; break;
      case b: h = ((r - g) / d + 4) / 6; break;
    }
  }
  return [h * 360, s, l];
}

function _hslToRgb(h, s, l) {
  h /= 360;
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const hue2rgb = (t) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1/6) return p + (q - p) * 6 * t;
    if (t < 1/2) return q;
    if (t < 2/3) return p + (q - p) * (2/3 - t) * 6;
    return p;
  };
  if (s === 0) { const v = Math.round(l * 255); return [v, v, v]; }
  return [hue2rgb(h + 1/3), hue2rgb(h), hue2rgb(h - 1/3)].map(v => Math.round(v * 255));
}

function _parse(hex) {
  const [r, g, b] = _hexToRgb(hex);
  return _rgbToHsl(r, g, b);
}

function _compose(h, s, l) {
  return _rgbToHex(..._hslToRgb(h, s, l));
}

/**
 * Add alpha to a hex color → rgba string.
 * alpha(C.a1, 0.12) → 'rgba(77,158,247,0.12)'
 */
export function alpha(hex, a) {
  const [r, g, b] = _hexToRgb(hex);
  return `rgba(${r},${g},${b},${a})`;
}

/**
 * Nudge lightness up by `amount` (0–1).
 * lighten('#4d9ef7', 0.15)
 */
export function lighten(hex, amount) {
  const [h, s, l] = _parse(hex);
  return _compose(h, s, Math.min(1, l + amount));
}

/**
 * Nudge lightness down by `amount` (0–1).
 * darken('#4d9ef7', 0.15)
 */
export function darken(hex, amount) {
  const [h, s, l] = _parse(hex);
  return _compose(h, s, Math.max(0, l - amount));
}

/**
 * Scale lightness by `factor` (0–1 = darker, >1 = lighter).
 * Useful for creating a "dimmer" variant that respects the original
 * lightness rather than subtracting a fixed amount.
 * dim(C.a1, 0.6) → 60% as bright
 */
export function dim(hex, factor) {
  const [h, s, l] = _parse(hex);
  return _compose(h, s, l * factor);
}

/**
 * Scale saturation by `factor`.
 * desaturate(C.a1, 0.5) → half as colourful
 */
export function desaturate(hex, factor) {
  const [h, s, l] = _parse(hex);
  return _compose(h, s * factor, l);
}

/**
 * Linear blend of two hex colors.
 * mix(C.a1, C.bgFloat, 0.15) → 15% a1, 85% bgFloat
 */
export function mix(hex1, hex2, t) {
  const [r1, g1, b1] = _hexToRgb(hex1);
  const [r2, g2, b2] = _hexToRgb(hex2);
  return _rgbToHex(
    r1 * t + r2 * (1 - t),
    g1 * t + g2 * (1 - t),
    b1 * t + b2 * (1 - t),
  );
}

/* ══════════════════════════════════════════════════════════
   BASE HUES
   One "main" hex per hue family. Everything else is derived.
   These are the only raw color values outside of backgrounds/
   borders/text — change these and the whole palette shifts.
   ══════════════════════════════════════════════════════════ */

const HUE = {
  blue:   '#4d9ef7',   // accent 1 — primary interactive
  cyan:   '#2dd4bf',   // secondary interactive / regex
  green:  '#3fb97a',   // accent 2 — strings / success
  teal:   '#2ab8a0',   // between green and cyan
  purple: '#a78bfa',   // types / classes
  pink:   '#f472b6',   // decorators / special
  amber:  '#f59e0b',   // numbers / constants
  orange: '#fb923c',   // built-ins / attributes
  red:    '#e05555',   // errors / danger
  yellow: '#facc15',   // warnings / annotations
};

/* ══════════════════════════════════════════════════════════
   FULL COLOR MAP  (C)
   Backgrounds, borders, text, and all hue variants.
   Derived values use the utilities above — nothing is
   manually duplicated.
   ══════════════════════════════════════════════════════════ */

export const C = {
  /* ── Backgrounds ─────────────────────────────────────── */
  bgRoot:     '#080a0e',
  bgFloat:    '#0d1017',
  bgElevated: '#111520',
  bgHover:    '#181f2e',
  bgActive:   '#141926',

  /* ── Borders ─────────────────────────────────────────── */
  border:      alpha('#ffffff', 0.06),
  borderHi:    alpha('#ffffff', 0.11),
  borderFocus: alpha('#ffffff', 0.14),

  /* ── Text ────────────────────────────────────────────── */
  textHi:  '#e8edf6',
  text:    '#8fa0b8',
  textDim: '#6b7a94',

  /* ── Blues ───────────────────────────────────────────── */
  blue:     HUE.blue,
  blueDim:  alpha(HUE.blue, 0.12),
  blueGlow: alpha(HUE.blue, 0.06),
  blueHi:   lighten(HUE.blue, 0.18),
  blueLo:   darken(HUE.blue, 0.15),

  /* ── Cyans ───────────────────────────────────────────── */
  cyan:    HUE.cyan,
  cyanDim: alpha(HUE.cyan, 0.12),
  cyanHi:  lighten(HUE.cyan, 0.18),
  cyanLo:  darken(HUE.cyan, 0.15),

  /* ── Greens ──────────────────────────────────────────── */
  green:     HUE.green,
  greenDim:  alpha(HUE.green, 0.12),
  greenHi:   lighten(HUE.green, 0.20),
  greenLo:   darken(HUE.green, 0.15),

  /* ── Teals ───────────────────────────────────────────── */
  teal:   HUE.teal,
  tealHi: lighten(HUE.teal, 0.20),
  tealLo: darken(HUE.teal, 0.15),

  /* ── Purples ─────────────────────────────────────────── */
  purple:    HUE.purple,
  purpleDim: alpha(HUE.purple, 0.12),
  purpleHi:  lighten(HUE.purple, 0.15),
  purpleLo:  darken(HUE.purple, 0.18),

  /* ── Pinks ───────────────────────────────────────────── */
  pink:   HUE.pink,
  pinkHi: lighten(HUE.pink, 0.15),
  pinkLo: darken(HUE.pink, 0.18),

  /* ── Ambers ──────────────────────────────────────────── */
  amber:    HUE.amber,
  amberDim: alpha(HUE.amber, 0.12),
  amberHi:  lighten(HUE.amber, 0.15),
  amberLo:  darken(HUE.amber, 0.20),

  /* ── Oranges ─────────────────────────────────────────── */
  orange:   HUE.orange,
  orangeHi: lighten(HUE.orange, 0.15),
  orangeLo: darken(HUE.orange, 0.18),

  /* ── Reds ────────────────────────────────────────────── */
  red:    HUE.red,
  redDim: alpha(HUE.red, 0.12),
  redHi:  lighten(HUE.red, 0.15),
  redLo:  darken(HUE.red, 0.20),

  /* ── Yellows ─────────────────────────────────────────── */
  yellow:   HUE.yellow,
  yellowLo: darken(HUE.yellow, 0.20),

  /* ── Legacy aliases (keep existing code working) ──────── */
  get a1()     { return this.blue; },
  get a1Dim()  { return this.blueDim; },
  get a1Glow() { return this.blueGlow; },
  get a2()     { return this.green; },
  get a2Dim()  { return this.greenDim; },
};

/* ══════════════════════════════════════════════════════════
   TYPOGRAPHY
   ══════════════════════════════════════════════════════════ */

export const FONT_MONO    = "'JetBrains Mono', 'IBM Plex Mono', 'Fira Mono', monospace";
export const FONT_UI      = "'Rubik', 'Inter', sans-serif";
export const FONT_HEADING = "'Poppins', 'Georgia', serif";

export const FS = {
  sm:   '0.70rem',
  base: '0.75rem',
  ui:   '0.8125rem',
  md:   '0.875rem',
};

/* ══════════════════════════════════════════════════════════
   CHROME
   ══════════════════════════════════════════════════════════ */

export const CHROME = {
  stripW:     '3.2rem',
  titleH:     '2rem',
  iconSize:   '2.9rem',
  btnSize:    '2.1rem',
  iconR:      '0.5rem',
  widgetR:    '0.625rem',
  gap:        '0.4rem',
  btnSvgSz:   '0.98em',
  stripSvgSz: '1.56em',
  thumbSvgSz: '1.32em',
};

/* ══════════════════════════════════════════════════════════
   MOTION
   ══════════════════════════════════════════════════════════ */

export const EASE_SINE = 'cubic-bezier(0.37, 0, 0.63, 1)';
export const EASE_IN   = 'cubic-bezier(0.55, 0, 1, 0.45)';
export const DUR       = '0.25s';

/* ══════════════════════════════════════════════════════════
   THEME INJECTION
   ══════════════════════════════════════════════════════════ */

const THEME_STYLE_ID = 'gimbal-theme';

export function injectTheme() {
  if (document.getElementById(THEME_STYLE_ID)) return;

  const vars = [
    /* backgrounds */
    ['--bg-root',      C.bgRoot],
    ['--bg-float',     C.bgFloat],
    ['--bg-elevated',  C.bgElevated],
    ['--bg-hover',     C.bgHover],
    ['--bg-active',    C.bgActive],

    /* borders */
    ['--border',       C.border],
    ['--border-hi',    C.borderHi],
    ['--border-focus', C.borderFocus],

    /* text */
    ['--text-hi',      C.textHi],
    ['--text',         C.text],
    ['--text-dim',     C.textDim],

    /* accents (legacy names stay for CSS consumers) */
    ['--a1',           C.blue],
    ['--a1-dim',       C.blueDim],
    ['--a1-glow',      C.blueGlow],
    ['--a2',           C.green],
    ['--a2-dim',       C.greenDim],

    /* full hue set */
    ['--blue',         C.blue],
    ['--blue-hi',      C.blueHi],
    ['--blue-lo',      C.blueLo],
    ['--cyan',         C.cyan],
    ['--cyan-hi',      C.cyanHi],
    ['--green',        C.green],
    ['--green-hi',     C.greenHi],
    ['--teal',         C.teal],
    ['--teal-hi',      C.tealHi],
    ['--purple',       C.purple],
    ['--purple-hi',    C.purpleHi],
    ['--pink',         C.pink],
    ['--pink-hi',      C.pinkHi],
    ['--amber',        C.amber],
    ['--amber-hi',     C.amberHi],
    ['--orange',       C.orange],
    ['--orange-hi',    C.orangeHi],
    ['--red',          C.red],
    ['--red-dim',      C.redDim],
    ['--red-hi',       C.redHi],
    ['--yellow',       C.yellow],

    /* typography */
    ['--font-ui',      FONT_UI],
    ['--font-mono',    FONT_MONO],
    ['--font-heading', FONT_HEADING],
    ['--fs-sm',        FS.sm],
    ['--fs-base',      FS.base],
    ['--fs-ui',        FS.ui],
    ['--fs-md',        FS.md],

    /* chrome */
    ['--strip-w',      CHROME.stripW],
    ['--title-h',      CHROME.titleH],
    ['--icon-size',    CHROME.iconSize],
    ['--btn-size',     CHROME.btnSize],
    ['--icon-r',       CHROME.iconR],
    ['--widget-r',     CHROME.widgetR],
    ['--gap',          CHROME.gap],
    ['--btn-svg-sz',   CHROME.btnSvgSz],
    ['--strip-svg-sz', CHROME.stripSvgSz],
    ['--thumb-svg-sz', CHROME.thumbSvgSz],

    /* motion */
    ['--ease-sine',    EASE_SINE],
    ['--ease-in',      EASE_IN],
    ['--dur',          DUR],
  ];

  injectStyles(THEME_STYLE_ID,
    `:root {\n${vars.map(([k, v]) => `  ${k}: ${v};`).join('\n')}\n}`
  );
}

/* ══════════════════════════════════════════════════════════
   UTILITIES
   ══════════════════════════════════════════════════════════ */

export function cssVar(name) {
  const prop = name.startsWith('--') ? name : `--${name}`;
  return getComputedStyle(document.documentElement).getPropertyValue(prop).trim();
}

export function injectStyles(id, css) {
  if (document.getElementById(id)) return;
  const s = document.createElement('style');
  s.id = id;
  s.textContent = css;
  document.head.appendChild(s);
}