export default function TaskDetails(props) {
  return (
    <aside id="task-details" data-task={props.task.id}>
      <strong>{props.task.title}</strong>
      <span>{props.task.done ? "Complete" : "In progress"}</span>
    </aside>
  );
}
