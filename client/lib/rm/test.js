// lib/rm/test.js
export const tests = [
  {
    label: 'rm removes a file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/hello.txt')`);
      await shell.eval(`rm('${scratch}/hello.txt')`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/hello.txt`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected file to be gone');
    },
  },
  {
    label: 'rm fails on a directory by default',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/subdir')`);
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/subdir')`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
  {
    label: 'rm with {r:1} removes a directory',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/subdir')`);
      await shell.eval(`rm('${scratch}/subdir', {r:1})`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/subdir`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory to be gone');
    },
  },
  {
    label: 'rm does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('hi').rm('${scratch}/hello.txt')`);
      } catch (e) {
        if (e.message.includes('pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'rm requires a path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('rm()');
      } catch (e) {
        if (e.message.includes('expected rm(path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },

  // ── multi-argument tests ────────────────────────────────────────────────────

  {
    label: 'rm removes multiple files',
    async fn(shell, scratch) {
      await shell.eval(`echo('a').to('${scratch}/a.txt')`);
      await shell.eval(`echo('b').to('${scratch}/b.txt')`);
      await shell.eval(`rm('${scratch}/a.txt', '${scratch}/b.txt')`);
      for (const name of ['a.txt', 'b.txt']) {
        let threw = false;
        try {
          const r = shell.resolvePath(`${scratch}/${name}`);
          await shell._vol(r.serverUrl, r.volume).lookup(r.path);
        } catch (e) {
          if (e.message.includes('not found')) threw = true;
          else throw e;
        }
        if (!threw) throw new Error(`expected ${name} to be gone`);
      }
    },
  },
  {
    label: 'rm with {r:1} removes multiple mixed paths',
    async fn(shell, scratch) {
      await shell.eval(`echo('x').to('${scratch}/x.txt')`);
      await shell.eval(`mkdir('${scratch}/dirmix')`);
      await shell.eval(`rm('${scratch}/x.txt', '${scratch}/dirmix', {r:1})`);
      for (const p of [`${scratch}/x.txt`, `${scratch}/dirmix`]) {
        let threw = false;
        try {
          const r = shell.resolvePath(p);
          await shell._vol(r.serverUrl, r.volume).lookup(r.path);
        } catch (e) {
          if (e.message.includes('not found')) threw = true;
          else throw e;
        }
        if (!threw) throw new Error(`expected ${p} to be gone`);
      }
    },
  },
  {
    label: 'rm fails fast: directory listed before a file leaves the file intact',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/early-dir')`);
      await shell.eval(`echo('c').to('${scratch}/c.txt')`);
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/early-dir', '${scratch}/c.txt')`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
      // c.txt must still exist — rm stopped before reaching it
      const r = shell.resolvePath(`${scratch}/c.txt`);
      await shell._vol(r.serverUrl, r.volume).lookup(r.path);
    },
  },
  {
    label: 'rm fails fast: file before directory is removed, then throws',
    async fn(shell, scratch) {
      await shell.eval(`echo('d').to('${scratch}/d.txt')`);
      await shell.eval(`mkdir('${scratch}/late-dir')`);
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/d.txt', '${scratch}/late-dir')`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
      // d.txt was processed first and must be gone
      let fileMissing = false;
      try {
        const r = shell.resolvePath(`${scratch}/d.txt`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) fileMissing = true;
        else throw e;
      }
      if (!fileMissing) throw new Error('expected d.txt to be gone');
    },
  },
  {
    label: 'rm rejects non-string arguments',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/x.txt', 42)`);
      } catch (e) {
        if (e.message.includes('expected rm(path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error for non-string arg');
    },
  },
  {
    label: 'rm fails on non-existent path by default',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/nope.txt')`);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not found error');
    },
  },
  {
    label: 'rm with {f:1} ignores non-existent path',
    async fn(shell, scratch) {
      await shell.eval(`rm('${scratch}/nope.txt', {f:1})`);
    },
  },
  {
    label: 'rm with {f:1} still fails on directory',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir')`);
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/dir', {f:1})`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error with {f:1}');
    },
  },
];
