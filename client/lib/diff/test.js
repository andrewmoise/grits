// lib/diff/test.js
export const tests = [
  {
    label: 'diff identical files via ln gives no output',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`ln('${scratch}/src.txt', '${scratch}/dest.txt')`);
      const text = await shell.eval(`diff('${scratch}/src.txt', '${scratch}/dest.txt').toText()`);
      if (text !== '')
        throw new Error(`expected empty output, got ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'diff different files produces output',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/a.txt')`);
      await shell.eval(`echo('world').to('${scratch}/b.txt')`);
      const text = await shell.eval(`diff('${scratch}/a.txt', '${scratch}/b.txt').toText()`);
      const lines = text.split('\n').filter(Boolean);
      if (lines.length !== 1)
        throw new Error(`expected 1 diff line, got ${lines.length}`);
      const parsed = JSON.parse(lines[0]);
      if (parsed[0] !== '.')
        throw new Error(`expected root path '.', got ${JSON.stringify(parsed[0])}`);
    },
  },
  {
    label: 'diff without {r:1} does not recurse',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/d1')`);
      await shell.eval(`mkdir('${scratch}/d2')`);
      await shell.eval(`echo('hello').to('${scratch}/d1/f.txt')`);
      await shell.eval(`echo('world').to('${scratch}/d2/f.txt')`);
      const text = await shell.eval(`diff('${scratch}/d1', '${scratch}/d2').toText()`);
      const lines = text.split('\n').filter(Boolean);
      if (lines.length !== 1)
        throw new Error(`expected exactly 1 diff line (no recurse), got ${lines.length}`);
      const parsed = JSON.parse(lines[0]);
      if (parsed[0] !== '.')
        throw new Error(`expected root path '.', got ${JSON.stringify(parsed[0])}`);
    },
  },
  {
    label: 'diff recursive finds nested differences',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/d1')`);
      await shell.eval(`mkdir('${scratch}/d1/sub')`);
      await shell.eval(`echo('one').to('${scratch}/d1/sub/x.txt')`);
      await shell.eval(`mkdir('${scratch}/d2')`);
      await shell.eval(`mkdir('${scratch}/d2/sub')`);
      await shell.eval(`echo('two').to('${scratch}/d2/sub/x.txt')`);
      const text = await shell.eval(`diff('${scratch}/d1', '${scratch}/d2', {r:1}).toText()`);
      const lines = text.split('\n').filter(Boolean);
      const paths = lines.map(l => JSON.parse(l)[0]);
      if (!paths.includes('sub/x.txt'))
        throw new Error(`expected sub/x.txt in diff, got ${JSON.stringify(paths)}`);
    },
  },
  {
    label: 'diff recursive with missing child on one side',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/d1')`);
      await shell.eval(`mkdir('${scratch}/d2')`);
      await shell.eval(`echo('only-a').to('${scratch}/d1/present.txt')`);
      const text = await shell.eval(`diff('${scratch}/d1', '${scratch}/d2', {r:1}).toText()`);
      const lines = text.split('\n').filter(Boolean);
      const entries = lines.map(l => JSON.parse(l));
      const presentEntry = entries.find(e => e[0] === 'present.txt');
      if (!presentEntry)
        throw new Error(`expected present.txt in diff, got ${JSON.stringify(entries.map(e => e[0]))}`);
      if (presentEntry[2] !== null)
        throw new Error(`expected null for right side of present.txt, got ${JSON.stringify(presentEntry[2])}`);
    },
  },
  {
    label: 'diff errors on non-existent path',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`diff('${scratch}/nonexistent', '${scratch}/other')`);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-found error');
    },
  },
];
