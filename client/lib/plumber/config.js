/**
 * @module lib/plumber/config.js
 * @about
 *   Plumber dispatch rules — this is the file to edit.
 *
 *   dispatch(msg, shell) is called for every plumbed message.
 *   Rules are evaluated top to bottom; return after handling.
 *   Unhandled messages fall through to the log at the bottom.
 *
 *   msg fields:
 *     src   — who sent it ('files', 'gterm', ...)
 *     dst   — intended destination, or '' if unspecified
 *     wdir  — working directory of the sender
 *     type  — 'text' (only type currently)
 *     attr  — object of extra metadata (mime, etc.)
 *     data  — the payload string
 */

const TEXT_EDITABLE = /\.(js|ts|mjs|css|html|md|json|txt|sh|py|rb|rs|go|c|h|cpp)$/i;

export async function dispatch(msg, shell) {

  // ── open editable file in editor ───────────────────────────
  if (msg.type === 'text' && TEXT_EDITABLE.test(msg.data)) {
    await shell.eval(`codemirror(${JSON.stringify(msg.data)})`);
    return;
  }

  // ── fallthrough ────────────────────────────────────────────
  console.log('[plumber] unhandled:', msg);
}