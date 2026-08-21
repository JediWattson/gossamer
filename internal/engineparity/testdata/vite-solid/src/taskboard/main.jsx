import {
  createResource,
  createSignal,
  For,
  lazy,
  onCleanup,
  Show,
  Suspense
} from "solid-js";
import { render } from "solid-js/web";

const TaskDetails = lazy(() => import("./task-details.jsx"));

async function loadTasks(filter) {
  await Promise.resolve();
  const board = await import("./board-data.js");
  return board.tasksFor(filter);
}

function Taskboard() {
  const [filter, setFilter] = createSignal("all");
  const [selected, setSelected] = createSignal(null);
  const [tasks] = createResource(filter, loadTasks);

  return (
    <section id="taskboard-app">
      <header>
        <h1>Strand taskboard</h1>
        <nav aria-label="Task filters">
          <button id="filter-all" onClick={() => setFilter("all")}>All</button>
          <button id="filter-open" onClick={() => setFilter("open")}>Open</button>
          <button id="filter-done" onClick={() => setFilter("done")}>Done</button>
        </nav>
      </header>
      <Suspense fallback={<p id="task-loading">Loading tasks</p>}>
        <p id="task-summary">{filter()}:{tasks()?.length ?? 0}</p>
        <ul id="task-list">
          <For each={tasks() ?? []}>{task => (
            <li data-task={task.id} classList={{ complete: task.done }}>
              <button id={`task-${task.id}`} onClick={() => setSelected(task)}>
                {task.title}
              </button>
            </li>
          )}</For>
        </ul>
      </Suspense>
      <Show when={selected()} keyed>
        {task => (
          <Suspense fallback={<p id="details-loading">Loading details</p>}>
            <TaskDetails task={task} />
          </Suspense>
        )}
      </Show>
    </section>
  );
}

globalThis.__solidTaskboardRuns = (globalThis.__solidTaskboardRuns || 0) + 1;
globalThis.__solidTaskboardCleanups = 0;
const mount = document.getElementById("solid-taskboard-root");
const dispose = render(() => {
  onCleanup(() => {
    globalThis.__solidTaskboardCleanups += 1;
  });
  return <Taskboard />;
}, mount);
globalThis.__solidTaskboardDispose = dispose;
