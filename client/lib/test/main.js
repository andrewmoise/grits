// lib/test/main.js
export const help = `\
testing — run all test.js suites found in :client/lib

Usage:
  testing()
  testing({v:1})    show full stack traces on failure`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const opts = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};

  const clientVol = shell.fs.volume(shell.serverUrl, 'client');

  const libDir = await clientVol.lookup('lib');
  if (!libDir.isDir())
    throw new Error('testing: cannot find lib directory');

  const children = await libDir.children();
  const suites = [];

  for (const [name, file] of children) {
    if (!file.isDir()) continue;
    const toolChildren = await file.children();
    if (!toolChildren.has('test.js')) continue;
    suites.push({ name });
  }

  if (suites.length === 0)
    return 'testing: no test.js files found in lib/*/';

  const lines = [];
  let totalPassed = 0, totalFailed = 0;
  const systemVol = shell.fs.volume(shell.serverUrl, 'sys');

  for (const { name } of suites) {
    lines.push(`\n[${name}]`);

    let mod;
    try {
      mod = await import(`../${name}/test.js`);
    } catch (e) {
      lines.push(`  IMPORT ERROR: ${e.message}`);
      if (opts.v) lines.push(e.stack ?? '  (no stack)');
      totalFailed++;
      continue;
    }

    if (!Array.isArray(mod.tests)) {
      lines.push(`  SKIP — no exported tests array`);
      continue;
    }

    let passed = 0, failed = 0;

    for (const { label, fn } of mod.tests) {
      const randSuffix = Math.random().toString(36).slice(2, 10);
      const scratchPath = `tmp/gimbal-test-${randSuffix}`;
      const emptyDir = await systemVol.mkdir();
      await systemVol.multiLink([{ path: scratchPath, addr: emptyDir }]);
      const scratch = `:sys/${scratchPath}`;

      try {
        await fn(shell, scratch);
        lines.push(`  ✓ ${label}`);
        passed++;
      } catch (e) {
        lines.push(`  ✗ ${label}: ${e.message}`);
        if (opts.v) lines.push((e.stack ?? '  (no stack)').split('\n').map(l => `    ${l}`).join('\n'));
        failed++;
      }

      // try {
      //   await systemVol.multiLink([{ path: scratchPath, addr: '' }]);
      // } catch (_) {}
    }

    lines.push(`  ${passed} passed, ${failed} failed`);
    totalPassed += passed;
    totalFailed += failed;
  }

  lines.push(`\n=== ${totalPassed} passed, ${totalFailed} failed ===`);
  if (totalFailed > 0) lines.push('SOME TESTS FAILED');

  return lines.join('\n');
}