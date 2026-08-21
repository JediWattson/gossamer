const tasks = [
  { id: "region-store", title: "Own native memory", done: true },
  { id: "module-graph", title: "Link module graphs", done: true },
  { id: "solid-app", title: "Boot a real Solid app", done: false }
];

function* matchingTasks(filter) {
  for (const task of tasks) {
    if (filter === "all" || (filter === "open" && !task.done) || (filter === "done" && task.done)) {
      yield task;
    }
  }
}

export function tasksFor(filter) {
  return [...matchingTasks(filter)];
}
