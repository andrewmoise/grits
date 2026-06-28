export const tests = [
  {
    label: 'js parses a JSON string from a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/data.json').w('{"a":1,"b":"two"}')`);
      const parsed = await gimbal.eval(`gimbal.p('${scratch}/data.json').read().js()`);
      if (typeof parsed !== 'object' || parsed === null)
        throw new Error('expected object');
      if (parsed.a !== 1) throw new Error(`expected a=1, got ${parsed.a}`);
      if (parsed.b !== 'two') throw new Error(`expected b="two", got ${parsed.b}`);
    },
  },
  {
    label: 'js parses a JSON array from a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/arr.json').w('[1,2,3]')`);
      const parsed = await gimbal.eval(`gimbal.p('${scratch}/arr.json').read().js()`);
      if (!Array.isArray(parsed)) throw new Error('expected array');
      if (parsed.length !== 3) throw new Error(`expected 3 items, got ${parsed.length}`);
      if (parsed[0] !== 1 || parsed[1] !== 2 || parsed[2] !== 3)
        throw new Error('unexpected array content');
    },
  },
  {
    label: 'js throws on invalid JSON',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/bad.json').w('{invalid')`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/bad.json').read().js()`);
      } catch (e) {
        if (e.message.includes('JSON')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected JSON parse error');
    },
  },
  {
    label: 'js throws on non-string prev',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').ls().js()`);
      } catch (e) {
        if (e.message.includes('expected a JSON string')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected type error');
    },
  },
];
