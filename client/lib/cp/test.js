export const tests = [
  {
    label: 'cp links a file to a new destination',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/echo.js'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/echo.js').read()`);
      if (!text.includes('invoke')) throw new Error('linked file content looks wrong');
    },
  },
  {
    label: 'cp into a directory places file inside it',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/subdir1').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/subdir1'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/subdir1/main.js').read()`);
      if (!text.includes('invoke')) throw new Error('file not found inside directory');
    },
  },
  {
    label: 'cp into existing directory places file inside',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/subdir2').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/subdir2'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/subdir2/main.js').read()`);
      if (!text.includes('invoke')) throw new Error('file not found inside directory');
    },
  },
  {
    label: 'cp overwrites an existing file by default',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke one')`);
      await gimbal.eval(`gimbal.p('${scratch}/other.js').w('invoke two')`);
      await gimbal.eval(`gimbal.p('${scratch}/other.js').cp(gimbal.p('${scratch}/main.js'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/main.js').read()`);
      if (!text.includes('invoke')) throw new Error('overwritten file content looks wrong');
    },
  },
  {
    label: 'cp into directory with {i:1} and overwrite',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke one')`);
      await gimbal.eval(`gimbal.p('${scratch}/other.js').w('invoke two')`);
      await gimbal.eval(`gimbal.p('${scratch}/subdir3').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/subdir3'), {i:1})`);
      const first = await gimbal.eval(`gimbal.p('${scratch}/subdir3/main.js').read()`);
      if (!first.includes('invoke')) throw new Error('first link content looks wrong');
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/subdir3'), {i:1})`);
      } catch (e) {
        if (e.message.includes('destination exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error on second link');
      await gimbal.eval(`gimbal.p('${scratch}/other.js').cp(gimbal.p('${scratch}/subdir3/main.js'))`);
      const third = await gimbal.eval(`gimbal.p('${scratch}/subdir3/main.js').read()`);
      if (!third.includes('invoke')) throw new Error('third link content looks wrong');
      if (third === first) throw new Error('third link did not overwrite');
    },
  },
  {
    label: 'cp with {f:1} respects directory semantics',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/subdir4').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/subdir4'), {f:1})`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/subdir4/main.js').read()`);
      if (!text.includes('invoke')) throw new Error('file not placed inside directory');
    },
  },
  {
    label: 'cp with {ff:1} overwrites a directory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/subdir5').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/subdir5'), {ff:1})`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/subdir5').read()`);
      if (!text.includes('invoke')) throw new Error('directory not overwritten by {ff:1}');
    },
  },
  {
    label: 'cp with {i:1} fails if destination exists',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/echo.js'))`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/echo.js'), {i:1})`);
      } catch (e) {
        if (e.message.includes('destination exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected destination exists error');
    },
  },
  {
    label: 'cp with {i:1} succeeds if destination does not exist',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/main.js').w('invoke test')`);
      await gimbal.eval(`gimbal.p('${scratch}/main.js').cp(gimbal.p('${scratch}/fresh.js'), {i:1})`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/fresh.js').read()`);
      if (!text.includes('invoke')) throw new Error('file content looks wrong');
    },
  },
  {
    label: 'cp does not accept pipeline input',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        // Calling .cp() after a pipeline (from another path) — cp rejects GimbalResult prev
        await gimbal.eval(`gimbal.p('${scratch}/f').w('x')`);
        await gimbal.eval(`gimbal.p('${scratch}/f').read().cp(gimbal.p('${scratch}/out.js'))`);
      } catch (e) {
        if (e.message.includes('need a source path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'cp with no arguments fails',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.cp()');
      } catch (e) {
        if (e.message.includes('need a source path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
];
