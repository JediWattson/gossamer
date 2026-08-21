# Pinned React taskboard fixture

This is a production Vite module graph built from React and ReactDOM 19.2.7.
It deliberately contains a shared runtime chunk, an asynchronous data chunk,
and a lazy component chunk so the browser gate covers normal module fetching,
linking, Promise scheduling, interaction, reconciliation, and teardown.

Rebuild it from this directory with:

```sh
npm ci
npm run build
```

The bundled React sources are MIT licensed; the pinned upstream license is in
`../../../../v8engine/testdata/react-LICENSE`.

The checked-in production chunks have these SHA-256 values, including their
trailing newlines:

```text
2b8a596d7f997958e8f59e26f69a55a2f13d4572089ddfafcda1d65e65d40df2  react-taskboard-19.2.7.production.module.js
c68f304bc44907b28b684aab65f0e642b11e840ee834607804049489508b70c9  react-taskboard-board-data-19.2.7.production.module.js
bbf7c5086ae414dc3f33777e3eebf16ea8631325b8bb4690d25091c03567f31a  react-taskboard-rolldown-runtime-19.2.7.production.module.js
03080e2ce216016a16e09e8608bb1b17507df6fcfeedc3d79df1b88fd4703a11  react-taskboard-runtime-19.2.7.production.module.js
dc192b9d4680cf5015dcd36ccbeb10de7efda54fb20c53f9e0f2bcb444b5915f  react-taskboard-task-details-19.2.7.production.module.js
```
