/*
 * @cell terminal-widget
 * @version 0.7
 * @about
 *   Interactive JS console for the Gimbal shell, built on jquery.terminal.
 *   Features: persistent shared scope, numbered results ($[n]/$$),
 *   tab completion from scope, async/await support.
 *   Depends on jQuery and jquery.terminal loaded from CDN on first use.
 * @implements gimbal-shell#widget
 */

const JQUERY_URL  = 'https://cdn.jsdelivr.net/npm/jquery@3.7.1/dist/jquery.min.js';
const JQTERM_JS   = 'https://cdn.jsdelivr.net/npm/jquery.terminal@2.x.x/js/jquery.terminal.min.js';
const JQTERM_CSS  = 'https://cdn.jsdelivr.net/npm/jquery.terminal@2.x.x/css/jquery.terminal.min.css';

function loadScript(src) {
  if (document.querySelector(`script[src="${src}"]`)) return Promise.resolve();
  return new Promise((res, rej) => {
    const s = document.createElement('script');
    s.src = src; s.onload = res; s.onerror = rej;
    document.head.appendChild(s);
  });
}
function loadCSS(href) {
  if (document.querySelector(`link[href="${href}"]`)) return;
  const l = document.createElement('link');
  l.rel = 'stylesheet'; l.href = href;
  document.head.appendChild(l);
}

function prettyFormat(val, depth = 0) {
  const PAD1 = '  '.repeat(depth + 1);
  const PAD  = '  '.repeat(depth);

  if (val === undefined) return '[[;#6b7080;]undefined]';
  if (val === null)      return '[[;#6b7080;]null]';
  if (typeof val === 'string')
    return `[[;#7ec8a0;]${$.terminal.escape_brackets(JSON.stringify(val))}]`;
  if (typeof val === 'number')  return `[[;#d4b866;]${val}]`;
  if (typeof val === 'boolean') return `[[;#a07ec8;]${val}]`;
  if (typeof val === 'function')
    return `[[i;#6b7080;][Function: ${val.name || 'ƒ'}]]`;

  const isArr = Array.isArray(val);
  const keys  = isArr ? [...val.keys()].map(String) : Object.keys(val);
  const [ob, cb] = isArr ? ['[',']'] : ['{','}'];

  if (!keys.length) return `[[;#6b7080;]${ob}${cb}]`;
  if (depth >= 2) {
    const preview = isArr
      ? `${ob}${keys.length} item${keys.length>1?'s':''}${cb}`
      : `${ob}${keys.slice(0,3).join(', ')}${keys.length>3?', …':''}${cb}`;
    return `[[;#6b7080;]${$.terminal.escape_brackets(preview)}]`;
  }

  const lines = [ob];
  keys.forEach((k, i) => {
    const v     = isArr ? val[i] : val[k];
    const comma = i < keys.length - 1 ? ',' : '';
    const entry = isArr
      ? `${PAD1}${prettyFormat(v, depth+1)}${comma}`
      : `${PAD1}[[;#5bc8c8;]${k}]: ${prettyFormat(v, depth+1)}${comma}`;
    lines.push(entry);
  });
  lines.push(`${PAD}${cb}`);
  return lines.join('\n');
}

export default async function createWidget({ name, evalContext = {} }) {
  // load CSS immediately, scripts deferred to mount()
  loadCSS(JQTERM_CSS);

  const el = document.createElement('div');
  el.className = 'gc-term';
  el.style.cssText = 'width:100%;height:100%;overflow:hidden;';

  // theme override — injected once
  const STYLE_ID = 'gimbal-jqterm-theme';
  if (!document.getElementById(STYLE_ID)) {
    const st = document.createElement('style');
    st.id = STYLE_ID;
    st.textContent = `
      .gc-term.terminal {
        background: #0e0f11 !important;
        font-family: "IBM Plex Mono","Fira Mono",monospace !important;
        font-size: 12px !important;
        line-height: 1.7 !important;
        padding: 8px 12px !important;
        height: 100% !important;
        box-sizing: border-box !important;
      }
      .gc-term .terminal-output .terminal-line { color: #c8cdd8; }
      .gc-term .cmd { color: #e8ecf4; }
      .gc-term .cmd .cursor { background: #5b8dd9; color: #0e0f11; }
      .gc-term .terminal-prompt,
      .gc-term .cmd-prompt { color: #5b8dd9 !important; }
      .gc-term.terminal::-webkit-scrollbar,
      .gc-term .terminal-output::-webkit-scrollbar { width: 3px; }
      .gc-term.terminal::-webkit-scrollbar-thumb,
      .gc-term .terminal-output::-webkit-scrollbar-thumb {
        background: #3d4150; border-radius: 2px;
      }
    `;
    document.head.appendChild(st);
  }

  // ── scope ─────────────────────────────────────────────
  const results = [];
  const store = Object.assign({}, evalContext, {
    get $()  { return results; },
    get $$() { return results[results.length - 1]; },
  });
  const scopeProxy = new Proxy(store, {
    has()        { return true; },
    get(t, k)    { return k in t ? t[k] : window[k]; },
    set(t, k, v) { t[k] = v; return true; },
  });

  function evalInContext(code) {
    const before = new Set(Object.keys(window));
    let result;
    try {
      // eslint-disable-next-line no-new-func
      result = new Function('__s__', `with(__s__) { return (${code}) }`)(scopeProxy);
    } catch(e) {
      if (!(e instanceof SyntaxError)) throw e;
      // eslint-disable-next-line no-new-func
      new Function('__s__', `with(__s__) { ${code} }`)(scopeProxy);
      result = undefined;
    }
    for (const k of Object.keys(window)) {
      if (!before.has(k)) store[k] = window[k];
    }
    return result;
  }

  let term = null;

  // ── mount — called by gimbal after el is in the DOM ───
  async function mount() {
    await loadScript(JQUERY_URL);
    await loadScript(JQTERM_JS);

    const $ = window.jQuery;

    term = $(el).terminal(async function(code) {
      if (!code.trim()) return;
      try {
        let result = evalInContext(code);
        if (result instanceof Promise) {
          this.pause();
          try   { result = await result; }
          finally { this.resume(); }
        }
        if (result !== undefined) {
          const i = results.length;
          results.push(result);
          this.echo(`[[;#3d4150;]← $${i}]  ${prettyFormat(result)}`, { raw: true });
        }
      } catch(err) {
        this.error(err.message ?? String(err));
      }
    }, {
      greetings: '[[b;#e8ecf4;]Gimbal] [[;#6b7080;]JS console]  [[;#5bc8c8;]help()]',
      prompt:    '[[;#5b8dd9;]›] ',
      checkArity: false,
      historySize: 500,
      completion(str, callback) {
        const allKeys = [...new Set([
          ...Object.keys(store),
          ...Object.getOwnPropertyNames(window).filter(k => k.length > 1),
        ])];
        const dotIdx = str.lastIndexOf('.');
        if (dotIdx !== -1) {
          const objPath = str.slice(0, dotIdx);
          const prefix  = str.slice(dotIdx + 1);
          try {
            const obj = evalInContext(objPath);
            if (obj != null) {
              const keys = [];
              let o = obj;
              while (o) { keys.push(...Object.getOwnPropertyNames(o)); o = Object.getPrototypeOf(o); }
              callback([...new Set(keys)]
                .filter(k => k.startsWith(prefix))
                .map(k => `${objPath}.${k}`));
              return;
            }
          } catch {}
        }
        callback(allKeys.filter(k => k.startsWith(str)));
      },
      keymap: {
        'CTRL+L'() { this.clear(); },
      },
      onInit() {
        store.clear = () => { this.clear(); return undefined; };
        store.help  = () => {
          this.echo([
            '[[b;#e8ecf4;]Gimbal JS Console]',
            '  [[;#5bc8c8;]ns]       namespace / filesystem',
            '  [[;#5bc8c8;]sh]       shell commands',
            '  [[;#5bc8c8;]$[n]]     result by index',
            '  [[;#5bc8c8;]$$]       last result',
            '  [[;#5bc8c8;]clear()]  clear terminal',
            '',
            'Tab=complete  ↑↓=history  Ctrl-R=search  Ctrl-L=clear',
          ].join('\n'), { raw: true });
          return undefined;
        };
      },
    });
  }

  return {
    el,
    mount,
    focus()   { term?.focus(); },
    destroy() { term?.destroy(); },
  };
}

