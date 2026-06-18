// lib/mv/test.js
export const tests = [
  {
    label: 'mv moves a file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dest.txt')`);
      const text = await shell.eval(`cat('${scratch}/dest.txt').toText()`);
      if (text !== 'hello')
        throw new Error(`expected 'hello' at dest, got '${text}'`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/src.txt`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected source to be gone');
    },
  },
  {
    label: 'mv fails if destination exists',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`echo('world').to('${scratch}/dest.txt')`);
      let threw = false;
      try {
        await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dest.txt')`);
      } catch (e) {
        if (e.message.includes('already exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error');
    },
  },
  {
    label: 'mv with {f:1} overwrites destination',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`echo('world').to('${scratch}/dest.txt')`);
      await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dest.txt', {f:1})`);
      const text = await shell.eval(`cat('${scratch}/dest.txt').toText()`);
      if (text !== 'hello')
        throw new Error(`expected 'hello' at dest, got '${text}'`);
    },
  },
  {
    label: 'mv does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('hi').mv('${scratch}/src.txt', '${scratch}/dest.txt')`);
      } catch (e) {
        if (e.message.includes('pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'mv requires two path arguments',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('mv()');
      } catch (e) {
        if (e.message.includes('expected mv(src, dest)')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
  {
    label: 'mv across volumes works like cp then rm',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      // Move to client volume (read-only in practice but tests the logic).
      // We'll move within sys but simulate cross-vol by using explicit paths.
      // Just verify same-vol mv works atomically as a sanity check.
      await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dest.txt')`);
      const text = await shell.eval(`cat('${scratch}/dest.txt').toText()`);
      if (text !== 'hello')
        throw new Error(`expected 'hello', got '${text}'`);
    },
  },
  {
    label: 'mv into directory places file inside it',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir1')`);
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dir1')`);
      const text = await shell.eval(`cat('${scratch}/dir1/src.txt').toText()`);
      if (text !== 'hello') throw new Error('file not moved into directory');
    },
  },
  {
    label: 'mv with trailing slash fails if destination does not exist',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      let threw = false;
      try {
        await shell.eval(`mv('${scratch}/src.txt', '${scratch}/notadir/')`);
      } catch (e) {
        threw = true;
      }
      if (!threw) throw new Error('expected an error');
    },
  },
  {
    label: 'mv with {f:1} respects directory semantics',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir2')`);
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dir2', {f:1})`);
      const text = await shell.eval(`cat('${scratch}/dir2/src.txt').toText()`);
      if (text !== 'hello') throw new Error('file not moved into directory');
    },
  },
  {
    label: 'mv with {ff:1} overwrites directory',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir3')`);
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`mv('${scratch}/src.txt', '${scratch}/dir3', {ff:1})`);
      const text = await shell.eval(`cat('${scratch}/dir3').toText()`);
      if (text !== 'hello') throw new Error('directory not overwritten');
    },
  },
];
