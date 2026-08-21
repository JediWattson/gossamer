import React, { lazy, Suspense, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";

const TaskDetails = lazy(() => import("./task-details.jsx"));

async function loadTasks(filter) {
  await Promise.resolve();
  const board = await import("./board-data.js");
  return board.tasksFor(filter);
}

function Taskboard() {
  const [filter, setFilter] = useState("all");
  const [tasks, setTasks] = useState(null);
  const [selected, setSelected] = useState(null);

  useEffect(() => {
    let active = true;
    setTasks(null);
    loadTasks(filter).then(nextTasks => {
      if (active) setTasks(nextTasks);
    });
    return () => {
      active = false;
      globalThis.__reactTaskboardEffectCleanups += 1;
    };
  }, [filter]);

  return (
    <section id="react-taskboard-app">
      <header>
        <h1>Strand React taskboard</h1>
        <nav aria-label="Task filters">
          <button id="react-filter-all" onClick={() => setFilter("all")}>All</button>
          <button id="react-filter-open" onClick={() => setFilter("open")}>Open</button>
          <button id="react-filter-done" onClick={() => setFilter("done")}>Done</button>
        </nav>
      </header>
      {tasks === null ? <p id="react-task-loading">Loading tasks</p> : (
        <>
          <p id="react-task-summary">{filter}:{tasks.length}</p>
          <ul id="react-task-list">
            {tasks.map(task => (
              <li key={task.id} data-task={task.id} className={task.done ? "complete" : ""}>
                <button id={`react-task-${task.id}`} onClick={() => setSelected(task)}>
                  {task.title}
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {selected !== null && (
        <Suspense fallback={<p id="react-details-loading">Loading details</p>}>
          <TaskDetails task={selected} />
        </Suspense>
      )}
    </section>
  );
}

globalThis.__reactTaskboardRuns = (globalThis.__reactTaskboardRuns || 0) + 1;
globalThis.__reactTaskboardEffectCleanups = 0;
const root = createRoot(document.getElementById("react-taskboard-root"));
flushSync(() => root.render(<Taskboard />));
globalThis.__reactTaskboardDispose = () => flushSync(() => root.unmount());
