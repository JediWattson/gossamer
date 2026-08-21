import { createSignal, For, onCleanup } from "solid-js";
import { render } from "solid-js/web";

globalThis.__solidCleanupCount = 0;
globalThis.__solidRowCleanups = 0;

function Row(props) {
  onCleanup(() => {
    globalThis.__solidRowCleanups += 1;
  });
  return (
    <li id={`solid-item-${props.item.id}`} data-key={props.item.id}>
      {props.item.label}
    </li>
  );
}

function SolidParityApp() {
  const [count, setCount] = createSignal(0);
  const [items, setItems] = createSignal([
    { id: "a", label: "Alpha" },
    { id: "b", label: "Beta" },
    { id: "c", label: "Gamma" }
  ]);
  onCleanup(() => {
    globalThis.__solidCleanupCount += 1;
  });
  return (
    <section id="solid-app">
      <button id="solid-counter" onClick={() => setCount(value => value + 1)}>
        Count {count()}
      </button>
      <button
        id="solid-reorder"
        onClick={() => setItems(current => [
          current[2],
          current[0],
          { id: "d", label: "Delta" }
        ])}
      >
        Reorder list
      </button>
      <ul id="solid-list">
        <For each={items()}>{item => <Row item={item} />}</For>
      </ul>
    </section>
  );
}

globalThis.__solidDispose = render(
  () => <SolidParityApp />,
  document.getElementById("solid-root")
);
globalThis.__solidReady = true;
