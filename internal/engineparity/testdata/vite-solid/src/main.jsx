import { createSignal, onCleanup } from "solid-js";
import { render } from "solid-js/web";

globalThis.__solidCleanupCount = 0;

function Counter() {
  const [count, setCount] = createSignal(0);
  onCleanup(() => {
    globalThis.__solidCleanupCount += 1;
  });
  return (
    <button id="solid-counter" onClick={() => setCount(value => value + 1)}>
      Count {count()}
    </button>
  );
}

globalThis.__solidDispose = render(
  () => <Counter />,
  document.getElementById("solid-root")
);
globalThis.__solidReady = true;
