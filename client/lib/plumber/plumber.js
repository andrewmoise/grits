/**
 * @module lib/plumber/plumber.js
 * @about
 *   Gimbal plumber — loosely modelled on Plan 9's plumber.
 *
 *   Any widget or tool calls evalContext.plumber.send(msg, shell) to
 *   route a message.  The dispatch logic lives entirely in config.js,
 *   which is the one file users are expected to edit.
 *
 *   Message format:
 *     {
 *       src:  'files',                     // who sent it
 *       dst:  '',                           // intended destination (hint only)
 *       wdir: shell.cwd,                   // working directory context
 *       type: 'text',                      // always 'text' for now
 *       attr: { mime: 'text/javascript' }, // optional metadata
 *       data: '/lib/foo/bar.js',           // the payload
 *     }
 *
 *   The shell passed to send() is the shell belonging to the sender —
 *   it carries the right cwd, evalContext, ui etc. for that context.
 *   config.js uses it to eval whatever command the rule decides on.
 */

let _dispatch = null;

async function _loadDispatch() {
  if (_dispatch) return _dispatch;
  const mod = await import('./config.js');
  _dispatch = mod.dispatch;
  return _dispatch;
}

export const plumber = {
  /**
   * Send a message through the plumber.
   * @param {object} msg   — plumber message (see format above)
   * @param {object} shell — GimbalShell from the calling context
   */
  async send(msg, shell) {
    const dispatch = await _loadDispatch();
    try {
      await dispatch(msg, shell);
    } catch (e) {
      console.error('[plumber] dispatch error:', e);
    }
  },
};