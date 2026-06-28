export const tests = [
  {
    label: 'to writes download Response to a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').w('hello world')`);
      const url = `${gimbal._serverUrl}/grits/v1/content/primary${scratch}/src.txt`;
      await gimbal.eval(`gimbal.download('${url}').to(gimbal.p('${scratch}/dest.txt'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/dest.txt').read()`);
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'to writes a string from echo to a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.echo('string content').to(gimbal.p('${scratch}/str_dest.txt'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/str_dest.txt').read()`);
      if (text !== 'string content')
        throw new Error(`expected 'string content', got '${text}'`);
    },
  },
  {
    label: 'to throws on non-string non-Response prev',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').to(gimbal.p('${scratch}/x'))`);
      } catch (e) {
        if (e.message.includes('expected a string or Response')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected prev type error');
    },
  },
  {
    label: 'to throws on array prev',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.echo([1,2,3]).to(gimbal.p('${scratch}/x'))`);
      } catch (e) {
        if (e.message.includes('expected a string or Response')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected prev type error');
    },
  },
  {
    label: 'to throws without destination',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.download('${gimbal._serverUrl}/grits/v1/content/primary/lib/grits/GritsClient.js').to()`);
      } catch (e) {
        if (e.message.includes('need a destination')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected destination error');
    },
  },
  {
    label: 'to with string dest works end-to-end',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/e2e_src.txt').w('e2e content')`);
      const url = `${gimbal._serverUrl}/grits/v1/content/primary${scratch}/e2e_src.txt`;
      const destPath = `${scratch}/e2e_dest.txt`;
      await gimbal.eval(`gimbal.download('${url}').to('${destPath}')`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/e2e_dest.txt').read()`);
      if (text !== 'e2e content')
        throw new Error(`expected 'e2e content', got '${text}'`);
    },
  },
];
