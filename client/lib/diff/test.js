export const tests = [
  {
    label: 'diff identical files via ln gives no output',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').ln(gsh.path('${scratch}/dest.txt'))`);
      const text = await shell.eval(`gsh.path('${scratch}/src.txt').diff(gsh.path('${scratch}/dest.txt'))`);
      if (text !== '')
        throw new Error(`expected empty output, got ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'diff different files produces output',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/a.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/b.txt').w('world')`);
      const text = await shell.eval(`gsh.path('${scratch}/a.txt').diff(gsh.path('${scratch}/b.txt'))`);
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
      await shell.eval(`gsh.path('${scratch}/d1').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/d2').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/d1/f.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/d2/f.txt').w('world')`);
      const text = await shell.eval(`gsh.path('${scratch}/d1').diff(gsh.path('${scratch}/d2'))`);
      const lines = text.split('\n').filter(Boolean);
      if (lines.length !== 1)
        throw new Error(`expected exactly 1 diff line (no recurse), got ${lines.length}`);
    },
  },
  {
    label: 'diff recursive finds nested differences',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/d1').mkdir({p:1})`);
      await shell.eval(`gsh.path('${scratch}/d1/sub').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/d1/sub/x.txt').w('one')`);
      await shell.eval(`gsh.path('${scratch}/d2').mkdir({p:1})`);
      await shell.eval(`gsh.path('${scratch}/d2/sub').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/d2/sub/x.txt').w('two')`);
      const text = await shell.eval(`gsh.path('${scratch}/d1').diff(gsh.path('${scratch}/d2'), {r:1})`);
      const lines = text.split('\n').filter(Boolean);
      const paths = lines.map(l => JSON.parse(l)[0]);
      if (!paths.includes('sub/x.txt'))
        throw new Error(`expected sub/x.txt in diff, got ${JSON.stringify(paths)}`);
    },
  },
  {
    label: 'diff recursive with missing child on one side',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/d1').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/d2').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/d1/present.txt').w('only-a')`);
      const text = await shell.eval(`gsh.path('${scratch}/d1').diff(gsh.path('${scratch}/d2'), {r:1})`);
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
        await shell.eval(`gsh.path('${scratch}/nonexistent').diff(gsh.path('${scratch}/other'))`);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-found error');
    },
  },
];
