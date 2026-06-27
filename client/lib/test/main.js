import { GimbalClient } from '../gimbal/client.js';
import { GimbalResult } from '../gimbal/result.js';

export const help = `\
test — run all test.js suites found in /lib

Usage:
  gimbal.test()                           — skip login/logout/whoami (prompting tests)
  gimbal.test({a:1})                      — run ALL suites including login/logout/whoami
  gimbal.test('login')                    — run only the login suite
  gimbal.test({v:1})                      — show full stack traces on failure
  gimbal.test({ff:1})                     — fail fast (immediately on first failure)`;

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalClient)) throw new Error('test: must be called on gimbal');

  const opts = isPlainObject(args[args.length - 1]) ? args.pop() : {};
  const namesFromArgs = args.filter(a => typeof a === 'string');
  const only = namesFromArgs.length > 0 ? new Set(namesFromArgs) : null;
  const skipDefault = !only && !opts.a ? new Set(['login', 'logout', 'whoami']) : null;
  const enc = new TextEncoder();

  return new GimbalResult(async () => {
    let controller;
    const stream = new ReadableStream({ start(c) { controller = c; } });
    const response = new Response(stream, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });

    const push = (line) => controller.enqueue(enc.encode(line + '\n'));

    try {
      const here = gimbal.grits.fromModule(import.meta.url);
      const vol = gimbal.grits.volume(here.serverUrl, here.volume);
      const parts = here.path.split('/');

      if (parts.length < 3 || parts[parts.length - 2] !== 'test') {
        push('test: must be located in a test/ directory');
        controller.close();
        return response;
      }

      const basePath = parts.slice(0, -2).join('/');
      const libDir = await vol.lookup(basePath);
      if (!libDir.isDir()) { push('test: cannot find lib directory'); controller.close(); return response; }

      const children = await libDir.children();
      const suites = [];

      for (const [name, file] of children) {
        if (!file.isDir()) continue;
        const toolChildren = await file.children();
        if (!toolChildren.has('test.js')) continue;
        if (!only || only.has(name)) {
          if (skipDefault && skipDefault.has(name)) continue;
          suites.push({ name });
        }
      }

      if (suites.length === 0) {
        push(only ? 'test: no matching test suites' : 'test: no test.js files found in lib/*/');
        controller.close();
        return response;
      }

      let totalPassed = 0, totalFailed = 0;
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

        if (!Array.isArray(mod.tests)) { push('  SKIP — no exported tests array'); continue; }

        let passed = 0, failed = 0;

        for (const { label, fn } of mod.tests) {
          const randSuffix = Math.random().toString(36).slice(2, 10);
          const scratchPath = `gimbal-test/${randSuffix}`;
          await gimbal.eval(`gimbal.p('/tmp/${scratchPath}').mkdir({p:1})`);
          const scratch = '/tmp/' + scratchPath;

          try {
            await fn(gimbal, scratch);
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
        if (bail) { push(`\n(bailed after first failure)`); break; }
      }

      push(`\n=== ${totalPassed} passed, ${totalFailed} failed ===`);
      if (totalFailed > 0) push('SOME TESTS FAILED');
    } catch (e) {
      push(`INTERNAL ERROR: ${e.message}`);
    } finally {
      controller.close();
    }

    return response;
  });
}
