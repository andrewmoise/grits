/*
 * @cell filebrowser-widget
 * @version 0.1
 * @about
 *   Stub file browser for the Gimbal shell. Renders a clickable tree of
 *   mock filesystem entries. Intended to be replaced by a real fs-backed
 *   implementation; the stub exists to exercise the widget contract and
 *   establish visual style.
 * @implements gimbal-shell#widget
 */

export default function createWidget({ name, evalContext = {} }) {
  const el = document.createElement('div');
  el.className = 'widget-body';
  el.style.cssText = 'padding:8px;overflow:auto;flex:1;min-height:0;font-size:12px;';

  // ── stub tree ────────────────────────────────────────────
  const tree = [
    { name: 'bin',        type: 'dir',  depth: 0 },
    { name: 'etc',        type: 'dir',  depth: 0 },
    { name: 'gimbal.html',type: 'file', depth: 0 },
    { name: 'usr',        type: 'dir',  depth: 0 },
    { name: 'lib',        type: 'dir',  depth: 1 },
    { name: 'share',      type: 'dir',  depth: 1 },
    { name: 'readme.md',  type: 'file', depth: 0 },
    { name: 'config.json',type: 'file', depth: 0 },
  ];

  let selected = null;

  function render() {
    el.innerHTML = tree.map((entry, i) => {
      const indent = entry.depth * 16;
      const icon   = entry.type === 'dir' ? '📁' : '📄';
      const isSelected = selected === i;
      return `<div data-i="${i}" style="
        padding: 3px 6px 3px ${6 + indent}px;
        border-radius: 3px;
        cursor: pointer;
        display: flex;
        align-items: center;
        gap: 6px;
        background: ${isSelected ? 'var(--bg3)' : 'transparent'};
        border: 1px solid ${isSelected ? 'var(--border)' : 'transparent'};
        color: ${isSelected ? 'var(--text-hi)' : 'var(--text)'};
        transition: background 0.1s;
      ">${icon} <span>${entry.name}</span></div>`;
    }).join('');

    // hover effect via event delegation
    el.querySelectorAll('[data-i]').forEach(row => {
      const i = parseInt(row.dataset.i);
      row.addEventListener('mouseenter', () => {
        if (selected !== i) row.style.background = 'var(--bg3)';
      });
      row.addEventListener('mouseleave', () => {
        if (selected !== i) row.style.background = 'transparent';
      });
      row.addEventListener('click', () => {
        selected = i;
        render();
      });
    });
  }

  render();

  return {
    el,
    focus() {},
    destroy() {},
  };
}

