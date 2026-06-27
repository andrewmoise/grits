const { Wasmer, init, initializeLogger, Directory } = await import("@wasmer/sdk");
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

const encoder = new TextEncoder();

async function main() {
    const wasmUrl = new URL('/lib/node_modules/@wasmer/sdk/dist/wasmer_js_bg.wasm', import.meta.url).href;
    await init({ module: wasmUrl });
    initializeLogger("debug");

    const term = new Terminal({ cursorBlink: true, convertEol: true });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(document.getElementById("terminal"));
    fit.fit();

    term.writeln("Loading sharrattj/bash...");

    const pkg = await Wasmer.fromRegistry("sharrattj/bash");
    term.reset();
    term.writeln("Starting bash...");

    const home = new Directory();

    const instance = await pkg.entrypoint.run({
        uses: ["wasmer/neatvi"],
        mount: { "/home": home },
        cwd: "/home",
    });

    term.writeln("Connected.");
    connectStreams(instance, term);
}

function connectStreams(instance, term) {
    const stdin = instance.stdin?.getWriter();
    term.onData(data => stdin?.write(encoder.encode(data)));
    instance.stdout.pipeTo(new WritableStream({ write: chunk => term.write(chunk) }));
    instance.stderr.pipeTo(new WritableStream({ write: chunk => term.write(chunk) }));
}

main().catch(err => {
    console.error(err);
    document.body.innerHTML = `<pre style="color:red">Error: ${err.message}\n${err.stack}</pre>`;
});
