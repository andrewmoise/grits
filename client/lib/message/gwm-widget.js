/*
 * @cell compose-widget  v0.1
 * @about
 *   Compose widget for sending messages to another user's inbox.
 *   Opens when message() is called without required arguments.
 * @implements gimbal-shell#widget
 */

import { FONT_MONO, injectStyles } from '../style/style.js';
import { toast } from '../gimbal/dialog.js';
import { sendMessage } from './send.js';

const STYLE_ID = 'gimbal-compose-styles';

const REGEX_USERNAME = /^[a-zA-Z][a-zA-Z0-9_]{2,31}$/;

function ensureStyles() {
  injectStyles(STYLE_ID, `
    .gc-wrap {
      padding: 1rem;
      font-family: var(--font-ui);
      font-size: var(--fs-md);
      color: var(--text);
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      box-sizing: border-box;
      height: 100%;
    }

    .gc-field-row {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      padding-left: 0.25rem;
    }
    .gc-field-row + .gc-field-row {
      margin-top: 0.15rem;
    }

    .gc-label {
      color: var(--a1);
      flex-shrink: 0;
      min-width: 3.5em;
    }

    .gc-input {
      font-family: var(--font-ui);
      font-size: var(--fs-md);
      padding: 0.35rem 0.5rem;
      border: 1px solid var(--border, rgba(255,255,255,0.15));
      border-radius: 0.3rem;
      background: var(--bg, #1a1a2e);
      color: var(--text);
      outline: none;
      transition: border-color 0.1s;
      flex: 1;
    }
    .gc-input:focus {
      border-color: var(--a2);
    }
    .gc-input.invalid {
      border-color: var(--red, #e53935);
    }
    .gc-input-mono {
      font-family: ${FONT_MONO};
    }

    .gc-input-from {
      font-family: ${FONT_MONO};
      font-size: var(--fs-md);
      padding: 0.35rem 0.5rem;
      border: 1px solid transparent;
      border-radius: 0.3rem;
      background: rgba(255,255,255,0.04);
      color: var(--text-dim);
      cursor: default;
      flex: 1;
    }

    .gc-body-wrap {
      flex: 1;
      display: flex;
      flex-direction: column;
      margin-top: 0.35rem;
    }
    .gc-body {
      flex: 1;
      resize: none;
      min-height: 6rem;
    }

    .gc-actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }

    .gc-btn {
      font-family: var(--font-ui);
      font-size: var(--fs-md);
      padding: 0.4rem 1rem;
      border: none;
      border-radius: 0.3rem;
      cursor: pointer;
      background: var(--a2);
      color: #fff;
      transition: opacity 0.1s;
    }
    .gc-btn:hover { opacity: 0.85; }
    .gc-btn:disabled {
      opacity: 0.35;
      cursor: default;
    }

    .gc-hint {
      font-size: var(--fs-md);
      color: var(--red, #e53935);
      min-height: 1em;
    }
  `);
}

export default function createWidget({ name, gimbal, args = [] }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gc-wrap';

  const serverUrl = gimbal?._serverUrl || window.location.origin;

  const prefill = (args && args[0]) || {};

  let from = '';
  let sending = false;

  const decoration = { title: '' };

  // ── Build form ───────────────────────────────────────────────

  const fromRow = fieldRow();
  fromRow.appendChild(fieldLabel('From'));
  const fromInput = document.createElement('div');
  fromInput.className = 'gc-input-from';
  fromRow.appendChild(fromInput);
  el.appendChild(fromRow);

  const toRow = fieldRow();
  toRow.appendChild(fieldLabel('To'));
  const toInput = document.createElement('input');
  toInput.className = 'gc-input gc-input-mono';
  toInput.type = 'text';
  toInput.placeholder = 'username';
  toInput.value = prefill.to || '';
  toRow.appendChild(toInput);
  el.appendChild(toRow);

  const subjectRow = fieldRow();
  subjectRow.appendChild(fieldLabel('Subject'));
  const subjectInput = document.createElement('input');
  subjectInput.className = 'gc-input';
  subjectInput.type = 'text';
  subjectInput.placeholder = '(optional)';
  subjectInput.value = prefill.subject || '';
  subjectRow.appendChild(subjectInput);
  el.appendChild(subjectRow);

  const bodyWrap = document.createElement('div');
  bodyWrap.className = 'gc-body-wrap';
  const bodyInput = document.createElement('textarea');
  bodyInput.className = 'gc-input gc-body';
  bodyInput.placeholder = 'Write your message...';
  bodyWrap.appendChild(bodyInput);
  el.appendChild(bodyWrap);

  const hintEl = document.createElement('div');
  hintEl.className = 'gc-hint';

  const actions = document.createElement('div');
  actions.className = 'gc-actions';

  const sendBtn = document.createElement('button');
  sendBtn.className = 'gc-btn';
  sendBtn.textContent = 'Send';
  sendBtn.disabled = true;
  actions.appendChild(sendBtn);

  el.appendChild(hintEl);
  el.appendChild(actions);

  // ── Get from field ───────────────────────────────────────────

  (async () => {
    try {
      const identities = await gimbal.grits.whoami(serverUrl);
      from = identities?.[0]?.username || '(anonymous)';
    } catch {
      from = '(anonymous)';
    }
    fromInput.textContent = from;
  })();

  // ── Validation ───────────────────────────────────────────────

  function validate() {
    const to = toInput.value.trim();
    const body = bodyInput.value.trim();

    const toValid = to.length > 0 && REGEX_USERNAME.test(to);
    const bodyValid = body.length > 0;
    const allValid = toValid && bodyValid;

    toInput.classList.toggle('invalid', to.length > 0 && !toValid);
    hintEl.textContent = (to.length > 0 && !toValid) ? 'Invalid username format' : '';
    sendBtn.disabled = !allValid;
  }

  toInput.addEventListener('input', validate);
  bodyInput.addEventListener('input', validate);
  validate();

  // ── Send ─────────────────────────────────────────────────────

  sendBtn.addEventListener('click', async () => {
    if (sending) return;
    sending = true;
    sendBtn.disabled = true;
    sendBtn.textContent = 'Sending...';

    const to = toInput.value.trim();
    const subject = subjectInput.value.trim();
    const body = bodyInput.value;

    try {
      const vol = gimbal.grits.volume(serverUrl, 'primary');
      await sendMessage(vol, to, from, subject, body);
      toast(`Message sent to ${to}`);
      toInput.value = '';
      subjectInput.value = '';
      bodyInput.value = '';
      hintEl.textContent = '';
      toInput.focus();
    } catch (e) {
      hintEl.textContent = `Couldn't deliver to ${to}: their inbox may not exist`;
      toInput.focus();
    }

    sending = false;
    sendBtn.textContent = 'Send';
    validate();
  });

  // ── Focus initial field ──────────────────────────────────────

  if (prefill.to) {
    subjectInput.focus();
  } else {
    toInput.focus();
  }

  return {
    el,
    decoration,
    focus() { toInput.focus(); },
    destroy() {},
  };
}

function fieldRow() {
  return Object.assign(document.createElement('div'), { className: 'gc-field-row' });
}

function fieldLabel(text) {
  const l = document.createElement('div');
  l.className = 'gc-label';
  l.textContent = text;
  return l;
}
