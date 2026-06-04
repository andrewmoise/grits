// lib/from/test.js
import { GritsFile } from '../grits/GritsClient.js';

export const tests = [
  {
    label: 'from with a path string returns a GritsFile',
    async fn(shell, scratch) {
      await shell.eval(`echo('export default 123').to('${scratch}/mod.js')`);
      const value = await shell.eval(`from('${scratch}/mod.js')`);
      if (!(value instanceof GritsFile))
        throw new Error(`expected GritsFile, got ${value?.constructor?.name}`);
    },
  },
  {
    label: 'from with a cross-volume path returns a GritsFile',
    async fn(shell, scratch) {
      await shell.eval(`echo('export default 123').to('${scratch}/mod.js')`);
      const value = await shell.eval(`from('${scratch}/mod.js')`);
      if (!(value instanceof GritsFile))
        throw new Error(`expected GritsFile, got ${value?.constructor?.name}`);
    },
  },
  {
    label: 'from preserves a GritsFile pass-through',
    async fn(shell, scratch) {
      await shell.eval(`echo('export default 123').to('${scratch}/mod.js')`);
      const file = await shell.eval(`from('${scratch}/mod.js')`);
      if (!(file instanceof GritsFile))
        throw new Error('expected GritsFile');
      // Pass it back in — should come out the same CID.
      const file2 = await shell.eval(`from('${scratch}/mod.js')`);
      if (!(file2 instanceof GritsFile))
        throw new Error('expected GritsFile from lookup');
      if (file2.cid() !== file.cid())
        throw new Error('CID changed on pass-through');
    },
  },
  {
    label: 'from preserves a Response pass-through',
    async fn(shell, scratch) {
      const value = await shell.eval(
        `from(await fetch('${shell.serverUrl}/grits/v1/content/client/lib/echo/main.js'))`
      );
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
    },
  },
  {
    label: 'from does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('export default 1').to('${scratch}/mod.js')`);
        await shell.eval(`echo('hi').from('${scratch}/mod.js')`);
      } catch (e) {
        if (e.message.includes('pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'from with no argument fails',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('from()');
      } catch (e) {
        if (e.message.includes('exactly one argument required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected argument required error');
    },
  },
  {
    label: 'from with multiple arguments fails',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('a').to('${scratch}/a.js')`);
        await shell.eval(`echo('b').to('${scratch}/b.js')`);
        await shell.eval(`from('${scratch}/a.js', '${scratch}/b.js')`);
      } catch (e) {
        if (e.message.includes('exactly one argument required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected single-argument error');
    },
  },
];
