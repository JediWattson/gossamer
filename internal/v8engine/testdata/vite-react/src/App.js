export function App() {
  const [count, setCount] = React.useState(0);
  return React.createElement(
    "section",
    {id: "production-app", "data-count": count},
    React.createElement("h1", null, "Gossamer production React"),
    React.createElement(
      "button",
      {
        id: "production-increment",
        onClick: () => ReactDOM.flushSync(() => setCount(value => value + 1)),
      },
      "Count ",
      count,
    ),
  );
}
