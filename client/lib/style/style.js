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
 *     import { injectTheme } from '/lib/style/style.js';
 *     injectTheme();
 *
 *     // widget JS
 *     import { FONT_MONO, C, injectStyles } from '../style/style.js';
 *     // C.bgFloat, C.textHi, C.a1, etc.
 */

/* ══════════════════════════════════════════════════════════
   COLORS
   ══════════════════════════════════════════════════════════ */

/** Raw color hex/rgba values — the one place these strings exist. */
export const C = {
  /* backgrounds */
  bgRoot:     '#080a0e',
  bgFloat:    '#0d1017',
  bgElevated: '#111520',
  bgHover:    '#181f2e',
  bgActive:   '#141926',

  /* borders */
  border:      'rgba(255,255,255,0.06)',
  borderHi:    'rgba(255,255,255,0.11)',
  borderFocus: 'rgba(255,255,255,0.14)',

  /* text */
  textHi:  '#e8edf6',
  text:    '#8fa0b8',
  textDim: '#6b7a94',

  /* accent 1 — blue */
  a1:     '#4d9ef7',
  a1Dim:  'rgba(77,158,247,0.12)',
  a1Glow: 'rgba(77,158,247,0.06)',

  /* accent 2 — green */
  a2:    '#3fb97a',
  a2Dim: 'rgba(63,185,122,0.12)',

  /* semantic */
  red:    '#e05555',
  redDim: 'rgba(224,85,85,0.12)',
};

/* ══════════════════════════════════════════════════════════
   TYPOGRAPHY
   ══════════════════════════════════════════════════════════ */

export const FONT_MONO = "'JetBrains Mono', 'IBM Plex Mono', 'Fira Mono', monospace";
export const FONT_UI   = "'Rubik', 'Inter', sans-serif";

/** Type scale — plain CSS values, used in JS strings and via var(--fs-*). */
export const FS = {
  sm:   '0.70rem',    /* 11.2px  status labels, tiny hints  */
  base: '0.75rem',    /* 12px    default mono / widget body  */
  ui:   '0.8125rem',  /* 13px    widget body (Rubik)         */
  md:   '0.875rem',   /* 14px    widget title                */
};

/* ══════════════════════════════════════════════════════════
   CHROME
   ══════════════════════════════════════════════════════════ */

export const CHROME = {
  stripW:  '2.8rem',
  titleH:  '2rem',
  iconSize:'2.4rem',
  btnSize: '1.75rem',
  iconR:   '0.5rem',
  widgetR: '0.625rem',
  gap:     '0.4rem',
};

/* ══════════════════════════════════════════════════════════
   MOTION
   ══════════════════════════════════════════════════════════ */

export const EASE_SINE = 'cubic-bezier(0.37, 0, 0.63, 1)';
export const EASE_IN   = 'cubic-bezier(0.55, 0, 1, 0.45)';
export const DUR       = '0.25s';

/* ══════════════════════════════════════════════════════════
   THEME INJECTION
   Writes every token above onto :root as CSS custom properties.
   Call once from the host page before anything renders.
   ══════════════════════════════════════════════════════════ */

const THEME_STYLE_ID = 'gimbal-theme';

export function injectTheme() {
  if (document.getElementById(THEME_STYLE_ID)) return;

  const vars = [
    /* colors */
    ['--bg-root',      C.bgRoot],
    ['--bg-float',     C.bgFloat],
    ['--bg-elevated',  C.bgElevated],
    ['--bg-hover',     C.bgHover],
    ['--bg-active',    C.bgActive],

    ['--border',       C.border],
    ['--border-hi',    C.borderHi],
    ['--border-focus', C.borderFocus],

    ['--text-hi',      C.textHi],
    ['--text',         C.text],
    ['--text-dim',     C.textDim],

    ['--a1',           C.a1],
    ['--a1-dim',       C.a1Dim],
    ['--a1-glow',      C.a1Glow],

    ['--a2',           C.a2],
    ['--a2-dim',       C.a2Dim],

    ['--red',          C.red],
    ['--red-dim',      C.redDim],

    /* typography */
    ['--font-ui',   FONT_UI],
    ['--font-mono', FONT_MONO],
    ['--fs-sm',     FS.sm],
    ['--fs-base',   FS.base],
    ['--fs-ui',     FS.ui],
    ['--fs-md',     FS.md],

    /* chrome */
    ['--strip-w',   CHROME.stripW],
    ['--title-h',   CHROME.titleH],
    ['--icon-size', CHROME.iconSize],
    ['--btn-size',  CHROME.btnSize],
    ['--icon-r',    CHROME.iconR],
    ['--widget-r',  CHROME.widgetR],
    ['--gap',       CHROME.gap],

    /* motion */
    ['--ease-sine', EASE_SINE],
    ['--ease-in',   EASE_IN],
    ['--dur',       DUR],
  ];

  injectStyles(THEME_STYLE_ID,
    `:root {\n${vars.map(([k, v]) => `  ${k}: ${v};`).join('\n')}\n}`
  );
}

/* ══════════════════════════════════════════════════════════
   UTILITIES
   ══════════════════════════════════════════════════════════ */

/**
 * Read a CSS custom property from :root at call time.
 * Useful when you need the computed value after any dynamic overrides,
 * or when you don't want to import the raw constant directly.
 */
export function cssVar(name) {
  const prop = name.startsWith('--') ? name : `--${name}`;
  return getComputedStyle(document.documentElement).getPropertyValue(prop).trim();
}

/**
 * Inject a <style> block exactly once, guarded by id.
 * Widgets call this in their factory so styles survive across
 * multiple instances without duplicating the tag.
 */
export function injectStyles(id, css) {
  if (document.getElementById(id)) return;
  const s = document.createElement('style');
  s.id = id;
  s.textContent = css;
  document.head.appendChild(s);
}