export const tests = [
  {
    label: 'cp copies a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').w('hello')`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').cp(gimbal.p('${scratch}/dest.txt'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/dest.txt').read()`);
      if (text !== 'hello') throw new Error('copy failed');
    },
  },
  {
    label: 'cp into directory places file inside',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/dir1').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').w('hello')`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').cp(gimbal.p('${scratch}/dir1'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/dir1/src.txt').read()`);
      if (text !== 'hello') throw new Error('not copied into directory');
    },
  },
  {
    label: 'cp with {f:1} respects directory semantics',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/dir2').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').w('hello')`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').cp(gimbal.p('${scratch}/dir2'), {f:1})`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/dir2/src.txt').read()`);
      if (text !== 'hello') throw new Error('not copied into directory');
    },
  },
  {
    label: 'cp with {ff:1} overwrites directory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/dir3').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').w('hello')`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').cp(gimbal.p('${scratch}/dir3'), {ff:1})`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/dir3').read()`);
      if (text !== 'hello') throw new Error('directory not overwritten');
    },
  },
];
