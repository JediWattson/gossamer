const tasks = [
  { id: "react-app", title: "Boot a real React app", done: false },
  { id: "module-graph", title: "Link the Vite module graph", done: true },
  { id: "teardown", title: "Release the React root", done: true }
];

export function tasksFor(filter) {
  if (filter === "open") return tasks.filter(task => !task.done);
  if (filter === "done") return tasks.filter(task => task.done);
  return tasks;
}
