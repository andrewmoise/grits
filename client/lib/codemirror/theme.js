/**
 * @module lib/codemirror/theme.js
 * @about
 *   CodeMirror syntax highlight style for the Gimbal editor.
 *   All colors derived from lib/style/style.js.
 *
 *   Tag entries are guarded — if a tag doesn't exist in the
 *   installed version of @lezer/highlight it is silently skipped
 *   rather than throwing. TODO: audit against pinned version.
 */

import { HighlightStyle, syntaxHighlighting } from 'https://esm.sh/@codemirror/language@6';
import { tags }                                from 'https://esm.sh/@lezer/highlight@1';
import { C, lighten, darken, dim, alpha, mix } from '../style/style.js';

/* ══════════════════════════════════════════════════════════
   SEMANTIC ROLES
   ══════════════════════════════════════════════════════════ */

export const SYN = {
  default:      C.textHi,
  dim:          C.textDim,
  subtle:       mix(C.textDim, C.bgFloat, 0.85),

  keyword:      C.blue,
  control:      C.blue,
  modifier:     C.cyanHi,
  operator:     C.cyan,
  punctuation:  mix(C.textDim, C.bgHover, 0.80),

  variable:     C.textHi,
  variableSpec: C.cyanHi,
  property:     C.text,
  parameter:    C.orangeHi,

  funcDef:      C.blueHi,
  classDef:     C.purpleHi,
  typeName:     C.purple,
  typeParam:    lighten(C.purple, 0.10),

  funcCall:     C.blueHi,
  namespace:    C.textHi,

  string:       C.greenHi,
  stringSpec:   C.tealHi,
  escape:       C.cyan,
  regexp:       C.teal,
  number:       C.amberHi,
  bool:         C.amber,
  null_:        C.amberLo,

  tagName:      C.blue,
  attrName:     C.orange,
  attrValue:    C.greenHi,
  doctype:      C.textDim,

  builtin:      C.orange,
  standard:     C.orangeHi,
  decorator:    C.pink,
  annotation:   C.yellow,

  comment:      dim(C.textDim, 0.85),
  docComment:   C.textDim,
  docTag:       C.blueLo,

  heading:      C.blueHi,
  strong:       C.textHi,
  emphasis:     C.text,
  link:         C.cyan,
  linkTitle:    C.cyanHi,
  code:         C.greenHi,
  list:         C.amberHi,
  quote:        C.textDim,
  meta:         C.textDim,

  inserted:     C.green,
  deleted:      C.red,
  changed:      C.amber,
  invalid:      C.redHi,
};

/* ══════════════════════════════════════════════════════════
   CODEMIRROR HIGHLIGHT STYLE
   t() guards against undefined tags — skips silently.
   ══════════════════════════════════════════════════════════ */

function t(tag, style) {
  return tag ? { tag, ...style } : null;
}

export const highlightStyle = HighlightStyle.define([
  // keywords
  t(tags.keyword,                                  { color: SYN.keyword,      fontWeight: '500' }),
  t(tags.controlKeyword,                           { color: SYN.control,      fontWeight: '500' }),
  t(tags.moduleKeyword,                            { color: SYN.keyword,      fontWeight: '500' }),
  t(tags.operatorKeyword,                          { color: SYN.keyword }),
  t(tags.modifier,                                 { color: SYN.modifier }),
  t(tags.self,                                     { color: SYN.variableSpec, fontStyle: 'italic' }),

  // operators & punctuation
  t(tags.operator,                                 { color: SYN.operator }),
  t(tags.compareOperator,                          { color: SYN.operator }),
  t(tags.logicOperator,                            { color: SYN.operator }),
  t(tags.arithmeticOperator,                       { color: SYN.operator }),
  t(tags.bitwiseOperator,                          { color: SYN.operator }),
  t(tags.updateOperator,                           { color: SYN.operator }),
  t(tags.definitionOperator,                       { color: SYN.operator }),
  t(tags.punctuation,                              { color: SYN.punctuation }),
  t(tags.bracket,                                  { color: SYN.punctuation }),
  t(tags.separator,                                { color: SYN.punctuation }),
  t(tags.derefOperator,                            { color: SYN.punctuation }),

  // names
  t(tags.variableName,                             { color: SYN.variable }),
  t(tags.definition(tags.variableName),            { color: SYN.variable }),
  t(tags.local?.(tags.variableName),               { color: SYN.variable }),
  t(tags.special?.(tags.variableName),             { color: SYN.variableSpec }),
  t(tags.propertyName,                             { color: SYN.property }),
  t(tags.definition(tags.propertyName),            { color: SYN.property }),
  t(tags.namespace,                                { color: SYN.namespace }),

  // function & class definitions
  t(tags.definition(tags.function(tags.variableName)), { color: SYN.funcDef,  fontWeight: '500' }),
  t(tags.function(tags.variableName),              { color: SYN.funcCall }),
  t(tags.function(tags.propertyName),              { color: SYN.funcCall }),
  t(tags.definition(tags.className),               { color: SYN.classDef,    fontWeight: '500' }),
  t(tags.className,                                { color: SYN.typeName }),

  // types
  t(tags.typeName,                                 { color: SYN.typeName }),
  t(tags.typeOperator,                             { color: SYN.keyword }),
  t(tags.typeVariable,                             { color: SYN.typeParam,    fontStyle: 'italic' }),

  // parameters
  t(tags.definition(tags.name),                    { color: SYN.parameter }),

  // values
  t(tags.string,                                   { color: SYN.string }),
  t(tags.special?.(tags.string),                   { color: SYN.stringSpec }),
  t(tags.docString,                                { color: SYN.string }),
  t(tags.character,                                { color: SYN.string }),
  t(tags.escape,                                   { color: SYN.escape }),
  t(tags.regexp,                                   { color: SYN.regexp }),
  t(tags.number,                                   { color: SYN.number }),
  t(tags.integer,                                  { color: SYN.number }),
  t(tags.float,                                    { color: SYN.number }),
  t(tags.bool,                                     { color: SYN.bool }),
  t(tags.null,                                     { color: SYN.null_ }),

  // built-ins
  t(tags.standard?.(tags.variableName),            { color: SYN.standard }),
  t(tags.standard?.(tags.name),                    { color: SYN.standard }),

  // attributes & decorators
  t(tags.annotation,                               { color: SYN.decorator }),
  t(tags.attributeName,                            { color: SYN.attrName }),
  t(tags.attributeValue,                           { color: SYN.attrValue }),

  // HTML / JSX tags
  t(tags.tagName,                                  { color: SYN.tagName,      fontWeight: '500' }),
  t(tags.docType,                                  { color: SYN.doctype }),
  t(tags.angleBracket,                             { color: SYN.punctuation }),

  // comments
  t(tags.comment,                                  { color: SYN.comment,      fontStyle: 'italic' }),
  t(tags.lineComment,                              { color: SYN.comment,      fontStyle: 'italic' }),
  t(tags.blockComment,                             { color: SYN.comment,      fontStyle: 'italic' }),
  t(tags.docComment,                               { color: SYN.docComment,   fontStyle: 'italic' }),
  t(tags.special?.(tags.comment),                  { color: SYN.docTag }),

  // markup (markdown)
  t(tags.heading,                                  { color: SYN.heading,      fontWeight: '500' }),
  t(tags.heading1,                                 { color: SYN.heading,      fontWeight: '700' }),
  t(tags.heading2,                                 { color: SYN.heading,      fontWeight: '600' }),
  t(tags.heading3,                                 { color: SYN.heading,      fontWeight: '500' }),
  t(tags.strong,                                   { color: SYN.strong,       fontWeight: '600' }),
  t(tags.emphasis,                                 { color: SYN.emphasis,     fontStyle: 'italic' }),
  t(tags.strikethrough,                            { color: SYN.dim,          textDecoration: 'line-through' }),
  t(tags.link,                                     { color: SYN.link,         textDecoration: 'underline' }),
  t(tags.url,                                      { color: SYN.linkTitle }),
  t(tags.monospace,                                { color: SYN.code }),
  t(tags.list,                                     { color: SYN.list }),
  t(tags.quote,                                    { color: SYN.quote,        fontStyle: 'italic' }),
  t(tags.meta,                                     { color: SYN.meta }),
  t(tags.processingInstruction,                    { color: SYN.meta }),

  // diff
  t(tags.inserted,                                 { color: SYN.inserted }),
  t(tags.deleted,                                  { color: SYN.deleted }),
  t(tags.changed,                                  { color: SYN.changed }),

  // diagnostics
  t(tags.invalid,                                  { color: SYN.invalid,      textDecoration: 'underline wavy' }),

].filter(Boolean));

export const syntaxTheme = syntaxHighlighting(highlightStyle);