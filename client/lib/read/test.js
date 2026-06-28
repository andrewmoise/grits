export const tests = [
  {
    label: 'read reads a file as a string',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/hello.txt').w('hello world')`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/hello.txt').read()`);
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'read reads a file from a subdirectory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/sub').mkdir({p:1})`);
      await gimbal.eval(`gimbal.p('${scratch}/sub/rel.txt').w('relative')`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/sub/rel.txt').read()`);
      if (text !== 'relative')
        throw new Error(`expected 'relative', got '${text}'`);
    },
  },
  {
    label: 'read reads a Response body as a string',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/resp_test.txt').w('from response')`);
      const url = `${gimbal._serverUrl}/grits/v1/content/primary${scratch}/resp_test.txt`;
      const text = await gimbal.eval(`gimbal.download('${url}').read()`);
      if (text !== 'from response')
        throw new Error(`expected 'from response', got '${text}'`);
    },
  },
  {
    label: 'read throws on invalid prev type',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.read(42)');
      } catch (e) {
        if (e.message.includes('expected a file path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected type error');
    },
  },
];
