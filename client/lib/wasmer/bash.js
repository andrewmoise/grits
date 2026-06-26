const { Wasmer, init, initializeLogger } = await import("@wasmer/sdk");
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

async function main() {
    await init();
    initializeLogger("debug");

    const term = new Terminal({ cursorBlink: true, convertEol: true });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(document.getElementById("terminal"));
    fit.fit();

    term.writeln("Loading sharrattj/bash...");

    const pkg = await Wasmer.fromRegistry("sharrattj/bash");
    term.writeln("Starting bash...");

    const instance = await pkg.entrypoint.run();

    term.writeln("Connected.");

    const encoder = new TextEncoder();
    const stdin = instance.stdin.getWriter();
    term.onData(data => stdin.write(encoder.encode(data)));
    instance.stdout.pipeTo(new WritableStream({ write: chunk => term.write(chunk) }));
    instance.stderr.pipeTo(new WritableStream({ write: chunk => term.write(chunk) }));
}

main().catch(err => {
    console.error(err);
    document.body.innerHTML = `<pre style="color:red">Error: ${err.message}\n${err.stack}</pre>`;
});
