/*
 * @cell dialog
 * @version 0.1
 * @about
 *   Toast notifications and modal input prompts for Gimbal widgets.
 *   Injects its own styles on first use per function.
 */

import { injectStyles } from '../style/style.js';

const TOAST_STYLE_ID = 'gimbal-toast-styles';
const PROMPT_STYLE_ID = 'gimbal-prompt-styles';

// ── Toast ────────────────────────────────────────────────
let _toastTimer = null;
let _toastEl = null;

function ensureToastStyles() {
  if (document.getElementById(TOAST_STYLE_ID)) return;
  injectStyles(TOAST_STYLE_ID, `
    .gimbal-toast-container {
      position: fixed;
      bottom: 1.5rem;
      left: 50%;
      transform: translateX(-50%);
      z-index: 2147483646;
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 0.4rem;
      pointer-events: none;
    }
    .gimbal-toast {
      background: var(--bg-elevated);
      border: 1px solid var(--border-hi);
      border-radius: 0.5rem;
      padding: 0.5rem 1rem;
      color: var(--text-hi);
      font-family: var(--font-ui);
      font-size: var(--fs-base);
      pointer-events: auto;
      cursor: pointer;
      box-shadow: 0 0.25rem 0.75rem rgba(0,0,0,0.45);
      animation: gimbal-toast-in 0.2s ease-out;
      transition: opacity 0.2s ease;
      max-width: min(30rem, 80vw);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .gimbal-toast.leaving {
      opacity: 0;
      transform: translateY(0.5rem);
    }
    @keyframes gimbal-toast-in {
      from { opacity: 0; transform: translateY(1rem); }
      to   { opacity: 1; transform: translateY(0); }
    }
  `);
}

export function toast(message, { duration = 3000 } = {}) {
  ensureToastStyles();

  let container = document.querySelector('.gimbal-toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'gimbal-toast-container';
    document.body.appendChild(container);
  }

  if (_toastEl) {
    _toastEl.remove();
    _toastEl = null;
    clearTimeout(_toastTimer);
  }

  const el = document.createElement('div');
  el.className = 'gimbal-toast';
  el.textContent = message;
  container.appendChild(el);
  _toastEl = el;

  el.addEventListener('click', () => dismissToast(el));

  _toastTimer = setTimeout(() => dismissToast(el), duration);
}

function dismissToast(el) {
  if (!el || !el.parentNode) return;
  el.classList.add('leaving');
  setTimeout(() => {
    el.remove();
    if (_toastEl === el) {
      _toastEl = null;
      clearTimeout(_toastTimer);
    }
  }, 200);
}

// ── Prompt ────────────────────────────────────────────────
let _promptResolve = null;

function ensurePromptStyles() {
  if (document.getElementById(PROMPT_STYLE_ID)) return;
  injectStyles(PROMPT_STYLE_ID, `
    .gimbal-prompt-backdrop {
      position: fixed; inset: 0;
      background: rgba(0,0,0,0.55);
      z-index: 2147483647;
      display: flex;
      align-items: center;
      justify-content: center;
      animation: gimbal-prompt-fadein 0.15s ease-out;
    }
    .gimbal-prompt-box {
      background: var(--bg-float);
      border: 1px solid var(--border-hi);
      border-radius: var(--widget-r);
      box-shadow: 0 0.5rem 2rem rgba(0,0,0,0.6);
      padding: 1.25rem;
      min-width: 20rem;
      max-width: 32rem;
      font-family: var(--font-ui);
    }
    .gimbal-prompt-message {
      color: var(--text-hi);
      font-size: var(--fs-md);
      margin-bottom: 0.75rem;
    }
    .gimbal-prompt-input {
      width: 100%;
      background: var(--bg-elevated);
      border: 1px solid var(--border);
      border-radius: 0.35rem;
      padding: 0.45rem 0.65rem;
      color: var(--text-hi);
      font-family: var(--font-mono);
      font-size: var(--fs-base);
      outline: none;
      transition: border-color 0.15s;
    }
    .gimbal-prompt-input:focus {
      border-color: var(--a1);
    }
    .gimbal-prompt-actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
      margin-top: 0.85rem;
    }
    .gimbal-prompt-btn {
      padding: 0.35rem 0.85rem;
      border-radius: 0.35rem;
      border: 1px solid var(--border);
      background: var(--bg-elevated);
      color: var(--text);
      font-family: var(--font-ui);
      font-size: var(--fs-base);
      cursor: pointer;
      transition: background 0.12s, color 0.12s;
    }
    .gimbal-prompt-btn:hover { background: var(--bg-hover); color: var(--text-hi); }
    .gimbal-prompt-btn.primary {
      background: var(--a1-dim);
      border-color: var(--a1);
      color: var(--a1);
    }
    .gimbal-prompt-btn.primary:hover { background: var(--a1); color: var(--text-hi); }
    .gimbal-prompt-btn.danger {
      background: var(--red-dim);
      border-color: var(--red);
      color: var(--red);
    }
    .gimbal-prompt-btn.danger:hover { background: var(--red); color: var(--text-hi); }
    @keyframes gimbal-prompt-fadein {
      from { opacity: 0; }
      to   { opacity: 1; }
    }
  `);
}

export function promptInput({ message, defaultValue = '' } = {}) {
  ensurePromptStyles();

  return new Promise(resolve => {
    _promptResolve = resolve;

    const backdrop = document.createElement('div');
    backdrop.className = 'gimbal-prompt-backdrop';

    const box = document.createElement('div');
    box.className = 'gimbal-prompt-box';

    const msgEl = document.createElement('div');
    msgEl.className = 'gimbal-prompt-message';
    msgEl.textContent = message;
    box.appendChild(msgEl);

    const input = document.createElement('input');
    input.className = 'gimbal-prompt-input';
    input.type = 'text';
    input.value = defaultValue;
    input.spellcheck = false;
    box.appendChild(input);

    const actions = document.createElement('div');
    actions.className = 'gimbal-prompt-actions';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'gimbal-prompt-btn';
    cancelBtn.textContent = 'Cancel';
    actions.appendChild(cancelBtn);

    const okBtn = document.createElement('button');
    okBtn.className = 'gimbal-prompt-btn primary';
    okBtn.textContent = 'OK';
    actions.appendChild(okBtn);

    box.appendChild(actions);
    backdrop.appendChild(box);
    document.body.appendChild(backdrop);

    function close(result) {
      backdrop.remove();
      _promptResolve = null;
      resolve(result);
    }

    cancelBtn.addEventListener('click', () => close(null));
    okBtn.addEventListener('click', () => close(input.value));
    input.addEventListener('keydown', e => {
      if (e.key === 'Enter') close(input.value);
      if (e.key === 'Escape') close(null);
    });

    // Focus input and select default value
    requestAnimationFrame(() => {
      input.focus();
      input.select();
    });
  });
}

// ── Confirm dialog ──────────────────────────────────────────
export function confirmDialog({ message }) {
  ensurePromptStyles();

  return new Promise(resolve => {
    const backdrop = document.createElement('div');
    backdrop.className = 'gimbal-prompt-backdrop';

    const box = document.createElement('div');
    box.className = 'gimbal-prompt-box';

    const msgEl = document.createElement('div');
    msgEl.className = 'gimbal-prompt-message';
    msgEl.textContent = message;
    box.appendChild(msgEl);

    const actions = document.createElement('div');
    actions.className = 'gimbal-prompt-actions';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'gimbal-prompt-btn';
    cancelBtn.textContent = 'Cancel';
    actions.appendChild(cancelBtn);

    const discardBtn = document.createElement('button');
    discardBtn.className = 'gimbal-prompt-btn danger';
    discardBtn.textContent = 'Discard';
    actions.appendChild(discardBtn);

    box.appendChild(actions);
    backdrop.appendChild(box);
    document.body.appendChild(backdrop);

    function close(result) {
      backdrop.remove();
      resolve(result);
    }

    cancelBtn.addEventListener('click', () => close(false));
    discardBtn.addEventListener('click', () => close(true));
    backdrop.addEventListener('keydown', e => {
      if (e.key === 'Escape') close(false);
    });

    discardBtn.focus();
  });
}
