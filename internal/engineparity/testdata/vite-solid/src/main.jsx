import { createSignal, For, onCleanup, Show } from "solid-js";
import { render } from "solid-js/web";

globalThis.__solidCleanupCount = 0;
globalThis.__solidRowCleanups = 0;
globalThis.__solidBranchCleanups = 0;

function VisibleBranch() {
  onCleanup(() => {
    globalThis.__solidBranchCleanups += 1;
  });
  return <p id="solid-visible">Visible branch</p>;
}

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
  const [visible, setVisible] = createSignal(true);
  const [name, setName] = createSignal("seed");
  const [enabled, setEnabled] = createSignal(false);
  const [choice, setChoice] = createSignal("alpha");
  const [pick, setPick] = createSignal("one");
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
      <button id="solid-toggle" onClick={() => setVisible(value => !value)}>
        Toggle branch
      </button>
      <Show when={visible()} fallback={<p id="solid-hidden">Hidden branch</p>}>
        <VisibleBranch />
      </Show>

      <label>
        Name
        <input
          id="solid-text"
          value={name()}
          onInput={event => setName(event.currentTarget.value)}
        />
      </label>
      <output id="solid-name">{name()}</output>

      <label>
        Enabled
        <input
          id="solid-check"
          type="checkbox"
          checked={enabled()}
          onChange={event => setEnabled(event.currentTarget.checked)}
        />
      </label>

      <label>
        Alpha
        <input
          id="solid-radio-alpha"
          type="radio"
          name="solid-choice"
          value="alpha"
          checked={choice() === "alpha"}
          onChange={event => event.currentTarget.checked && setChoice("alpha")}
        />
      </label>
      <label>
        Beta
        <input
          id="solid-radio-beta"
          type="radio"
          name="solid-choice"
          value="beta"
          checked={choice() === "beta"}
          onChange={event => event.currentTarget.checked && setChoice("beta")}
        />
      </label>

      <select
        id="solid-select"
        value={pick()}
        onChange={event => setPick(event.currentTarget.value)}
      >
        <option value="one">One</option>
        <option value="two">Two</option>
      </select>

      <output id="solid-form-state">
        {name()}:{enabled() ? "enabled" : "disabled"}:{choice()}:{pick()}
      </output>

      <div
        id="solid-dynamic"
        classList={{ active: enabled() }}
        style={{
          color: enabled() ? "red" : "blue",
          "--solid-count": String(count())
        }}
        data-state={enabled() ? "on" : "off"}
        title={`count-${count()}`}
        hidden={!visible()}
      >
        Dynamic surface
      </div>
    </section>
  );
}

globalThis.__solidDispose = render(
  () => <SolidParityApp />,
  document.getElementById("solid-root")
);
globalThis.__solidReady = true;
