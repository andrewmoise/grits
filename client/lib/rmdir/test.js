export const tests = [
  {
    label: 'rmdir removes an empty directory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/emptydir').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/emptydir').rmdir()`);
      let threw = false;
      try {
        const r = gimbal.resolvePath(`${scratch}/emptydir`);
        await gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory to be gone');
    },
  },
  {
    label: 'rmdir removes a non-empty directory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/fulldir').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/fulldir/file.txt').w('hi')`);
      await gimbal.eval(`gimbal.p('${scratch}/fulldir').rmdir()`);
      let threw = false;
      try {
        const r = gimbal.resolvePath(`${scratch}/fulldir`);
        await gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory to be gone');
    },
  },
  {
    label: 'rmdir fails on a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/file.txt').w('hi')`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/file.txt').rmdir()`);
      } catch (e) {
        if (e.message.includes('not a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-a-directory error');
    },
  },
  {
    label: 'rmdir does not accept pipeline input',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/d').mkdir()`);
        await gimbal.eval(`gimbal.p('${scratch}/d').read().rmdir()`);
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected error');
    },
  },
  {
    label: 'rmdir requires a path argument',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.rmdir()');
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
];
