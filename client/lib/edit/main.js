export const help = `edit [path|object] — open a file in the editor`;

export async function invoke(shell, previous, args) {
  // Directly dispatch to codemirror via shell proxy
  return shell.runCommand('codemirror', args, {doHistory: false});
}
