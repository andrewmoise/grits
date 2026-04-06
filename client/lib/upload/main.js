// lib/upload/main.js
export const help = `\
upload — open a file picker and bring the selected file into the pipeline

Usage:
  upload()
  upload({ accept: 'image/*' })
  upload().to('local-file.json')

Output is a Response wrapping the selected file's contents.
Error thrown if the user cancels or no file is selected.`;

import { isVoid } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('upload: does not accept pipeline input — use as entry point only');

  const [opts = {}] = args;

  const file = await pickFile(opts.accept);
  return new Response(file, {
    headers: { 'Content-Type': file.type || 'application/octet-stream' },
  });
}

function pickFile(accept) {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input');
    input.type = 'file';
    if (accept) input.accept = accept;

    let settled = false;

    input.addEventListener('change', () => {
      settled = true;
      const file = input.files?.[0];
      if (file) resolve(file);
      else reject(new Error('upload: no file selected'));
    });

    input.click();
  });
}