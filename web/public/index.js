const go = new Go();

const output = document.getElementById("output");
const input = document.getElementById("input");

WebAssembly.instantiateStreaming(
    fetch("engine.wasm"),
    go.importObject
).then(result => {
    go.run(result.instance); // deliberately not awaited — see api-contract.md
    input.disabled = false;
    input.placeholder = "";
    input.focus();
});

input.addEventListener("keydown", (event) => {
    if (event.key !== "Enter") return;
    const line = input.value;
    input.value = "";

    const res = window.execute(line);
    output.textContent += `$ ${line}\n${res.stdout}${res.stderr}`;
});

