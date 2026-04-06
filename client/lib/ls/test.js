// lib/ls/test.js
export const tests = [
  {
    label: 'ls lists current directory as JSON array',
    async fn(shell, scratch) {
      const value = await shell.eval(`ls('${scratch}')`);
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
      const list = await value.json();
      if (!Array.isArray(list))
        throw new Error(`expected array, got ${typeof list}`);
    },
  },
  {
    label: 'ls shows files that exist in directory',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/hello.txt')`);
      const list = await (await shell.eval(`ls('${scratch}')`)).json();
      if (!list.includes('hello.txt'))
        throw new Error(`expected hello.txt in listing, got ${JSON.stringify(list)}`);
    },
  },
  {
    label: 'ls returns sorted results',
    async fn(shell, scratch) {
      await shell.eval(`echo('a').to('${scratch}/c.txt')`);
      await shell.eval(`echo('b').to('${scratch}/a.txt')`);
      await shell.eval(`echo('c').to('${scratch}/b.txt')`);
      const list = await (await shell.eval(`ls('${scratch}')`)).json();
      if (JSON.stringify(list) !== JSON.stringify([...list].sort()))
        throw new Error(`expected sorted list, got ${JSON.stringify(list)}`);
    },
  },
  {
    label: 'ls with {l:true} returns metadata map',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/hello.txt')`);
      const value = await shell.eval(`ls('${scratch}', {l:true})`);
      const entries = await value.json();
      if (typeof entries !== 'object' || Array.isArray(entries))
        throw new Error(`expected object, got ${typeof entries}`);
      if (!entries['hello.txt'])
        throw new Error('expected hello.txt in long listing');
      if (!entries['hello.txt'].type)
        throw new Error('expected metadata with type field');
    },
  },
  {
    label: 'ls with no args lists cwd',
    async fn(shell, scratch) {
      // cd into scratch, ls, cd back
      const savedCwd    = shell.cwd;
      const savedVol    = shell.volume;
      const savedServer = shell.serverUrl;
      const r = shell.resolvePath(scratch);
      shell.serverUrl = r.serverUrl;
      shell.volume    = r.volume;
      shell.cwd       = r.path;

      try {
        await shell.eval(`echo('hi').to('${scratch}/hi.txt')`);
        const list = await (await shell.eval('ls()')).json();
        if (!list.includes('hi.txt'))
          throw new Error(`expected hi.txt in cwd listing`);
      } finally {
        shell.serverUrl = savedServer;
        shell.volume    = savedVol;
        shell.cwd       = savedCwd;
      }
    },
  },
  {
    label: 'ls on a file fails',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/hello.txt')`);
      let threw = false;
      try {
        await shell.eval(`ls('${scratch}/hello.txt')`);
      } catch (e) {
        if (e.message.includes('not a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-a-directory error');
    },
  },
  {
    label: 'ls cannot combine pipeline input with path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('hi').ls('${scratch}')`);
      } catch (e) {
        if (e.message.includes('cannot combine')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected combine error');
    },
  },
];