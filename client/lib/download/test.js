export const tests = [
  {
    label: 'download result matches same file fetched from volume',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/file.txt').w('hello world')`);
      const url = `${shell.serverUrl}/grits/v1/content/primary${scratch}/file.txt`;

      const downloadResult = await shell.eval(`gsh.download('${url}')`);
      const fetchedResp = await fetch(url);
      const a = await downloadResult.text();
      const b = await fetchedResp.text();
      const c = await shell.eval(`gsh.p('${scratch}/file.txt').read()`);

      if (a !== b || a !== c)
        throw new Error('downloaded content does not match volume content');
    },
  },
  {
    label: 'download does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.p('${scratch}/x').download('${shell.serverUrl}/grits/v1/content/primary/lib/grits/GritsClient.js')`);
      } catch (e) {
        if (e.message.includes('must be called on gsh')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'download requires a URL argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.download()');
      } catch (e) {
        if (e.message.includes('URL string required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected URL required error');
    },
  },
  {
    label: 'download requires a string URL, not other types',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.download(42)');
      } catch (e) {
        if (e.message.includes('URL string required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected URL string error');
    },
  },
];
