export const help = `\
testing — run all test.js suites found in //client/lib

Usage:
  testing()
  testing({v:1})       show full stack traces on failure
  testing({ff:1})    fail fast (immediately on first failure)`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const opts = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const namesFromArgs = args.filter(a => typeof a === 'string');
  const only = namesFromArgs.length > 0 ? new Set(namesFromArgs) : null;
  const enc = new TextEncoder();

  let controller;
  const stream = new ReadableStream({ start(c) { controller = c; } });
  const response = new Response(stream, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });

  const push = (line) => controller.enqueue(enc.encode(line + '\n'));

  (async () => {
    try {
      // Derive location from this module instead of hardcoding //client/lib
      const here = shell.fs.fromModule(import.meta.url);
      const vol  = shell.fs.volume(here.serverUrl, here.volume);

      const parts = here.path.split('/');
      // Expect .../test/main.js
      if (parts.length < 3 || parts[parts.length - 2] !== 'test') {
        push('testing: must be located in a test/ directory');
        controller.close();
        return;
      }

      // Strip "test/main.js" → parent directory (e.g. lib/)
      const basePath = parts.slice(0, -2).join('/');

      const libDir = await vol.lookup(basePath);
      if (!libDir.isDir()) { push('testing: cannot find lib directory'); controller.close(); return; }

      const children = await libDir.children();
      const suites = [];

      for (const [name, file] of children) {
        if (!file.isDir()) continue;
        const toolChildren = await file.children();
        if (!toolChildren.has('test.js')) continue;
        if (!only || only.has(name)) {
          suites.push({ name });
        }
      }

      if (suites.length === 0) {
        if (only) {
          push('testing: no matching test suites');
        } else {
          push('testing: no test.js files found in lib/*/');
        }
        controller.close();
        return;
      }

      let totalPassed = 0, totalFailed = 0;
      const systemVol = shell.fs.volume(shell.serverUrl, 'sys');
      let bail = false;

      for (const { name } of suites) {
        if (bail) break;
        push(`\n[${name}]`);

        let mod;
        try {
          mod = await import(`../${name}/test.js`);
        } catch (e) {
          push(`  IMPORT ERROR: ${e.message}`);
          if (opts.v) push(e.stack ?? '  (no stack)');
          totalFailed++;
          if (opts.ff) { bail = true; break; }
          continue;
        }

        if (!Array.isArray(mod.tests)) {
          push(`  SKIP — no exported tests array`);
          continue;
        }

        let passed = 0, failed = 0;

        for (const { label, fn } of mod.tests) {
          const randSuffix = Math.random().toString(36).slice(2, 10);
          const scratchPath = `tmp/gimbal-test/${randSuffix}`;
          // Ensure path exists using mkdir -p semantics
          await shell.eval(`mkdir('//sys/${scratchPath}', {p:1})`);
          const scratch = `//sys/${scratchPath}`;

          try {
            await fn(shell, scratch);
            push(`  ✓ ${label}`);
            passed++;
          } catch (e) {
            push(`  ✗ ${label}: ${e.message}`);
            if (opts.v) push((e.stack ?? '  (no stack)').split('\n').map(l => `    ${l}`).join('\n'));
            failed++;
            if (opts.ff) { bail = true; break; }
          }
        }

        push(`  ${passed} passed, ${failed} failed`);
        totalPassed += passed;
        totalFailed += failed;

        if (bail) {
          push(`\n(bailed after first failure)`);
          break;
        }
      }

      push(`\n=== ${totalPassed} passed, ${totalFailed} failed ===`);
      if (totalFailed > 0) push('SOME TESTS FAILED');
    } catch (e) {
      push(`INTERNAL ERROR: ${e.message}`);
    } finally {
      controller.close();
    }
  })();

  return response;
}
