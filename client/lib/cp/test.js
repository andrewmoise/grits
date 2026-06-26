export const tests = [
  {
    label: 'cp copies a file',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').cp(gsh.path('${scratch}/dest.txt'))`);
      const text = await shell.eval(`gsh.path('${scratch}/dest.txt').read()`);
      if (text !== 'hello') throw new Error('copy failed');
    },
  },
  {
    label: 'cp into directory places file inside',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir1').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').cp(gsh.path('${scratch}/dir1'))`);
      const text = await shell.eval(`gsh.path('${scratch}/dir1/src.txt').read()`);
      if (text !== 'hello') throw new Error('not copied into directory');
    },
  },
  {
    label: 'cp with {f:1} respects directory semantics',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir2').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').cp(gsh.path('${scratch}/dir2'), {f:1})`);
      const text = await shell.eval(`gsh.path('${scratch}/dir2/src.txt').read()`);
      if (text !== 'hello') throw new Error('not copied into directory');
    },
  },
  {
    label: 'cp with {ff:1} overwrites directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir3').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').cp(gsh.path('${scratch}/dir3'), {ff:1})`);
      const text = await shell.eval(`gsh.path('${scratch}/dir3').read()`);
      if (text !== 'hello') throw new Error('directory not overwritten');
    },
  },
];
