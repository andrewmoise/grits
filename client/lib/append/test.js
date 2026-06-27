export const tests = [
  {
    label: 'append adds content to the end of an existing file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/append.txt').w('hello ')`);
      await gimbal.eval(`gimbal.p('${scratch}/append.txt').append('world')`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/append.txt').read()`);
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'append creates file if it does not exist',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/new.txt').append('new file')`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/new.txt').read()`);
      if (text !== 'new file')
        throw new Error(`expected 'new file', got '${text}'`);
    },
  },
  {
    label: 'append requires content',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/f.txt').append()`);
      } catch (e) {
        if (e.message.includes('no content')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected content error');
    },
  },
  {
    label: 'append compounds across multiple calls',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/compound.txt').w('a')`);
      await gimbal.eval(`gimbal.p('${scratch}/compound.txt').append('b')`);
      await gimbal.eval(`gimbal.p('${scratch}/compound.txt').append('c')`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/compound.txt').read()`);
      if (text !== 'abc')
        throw new Error(`expected 'abc', got '${text}'`);
    },
  },
  {
    label: 'append errors on directory destination',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/append_dir').mkdir()`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/append_dir').append('data')`);
      } catch (e) {
        if (e.message.includes('directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
];
