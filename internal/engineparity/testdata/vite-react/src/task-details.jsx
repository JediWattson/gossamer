export default function TaskDetails({ task }) {
  return (
    <aside id="react-task-details" data-task={task.id}>
      <strong>{task.title}</strong>
      <span>{task.done ? "Complete" : "In progress"}</span>
    </aside>
  );
}
