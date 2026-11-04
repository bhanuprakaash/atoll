# React style

Condensed from [Bulletproof React](https://github.com/alan2207/bulletproof-react/tree/master/docs).
Where this file and the source differ, this file wins. Add each piece of the stack only when a
ticket needs it.

## Stack

- Vite, React, TypeScript with `strict: true`, React Router, TanStack Query. pnpm for packages.
- Tailwind v4 utility classes. Colours, radii and fonts are tokens in the `@theme` block of
  `src/index.css`. Use token classes (`bg-card`, `text-muted`), never hex values or `dark:`
  variants, and the shared pieces in `components/` instead of hand-rolled controls.
- No Zod, form library or global store library.

## Structure

The app lives in `web/src`:

```text
app/          app setup, router, composition of modules
components/   shared UI components
lib/          configured libraries, including the API client
modules/      code grouped by Reef area (bots, threads, settings, ...)
types/        shared TypeScript types
utils/        shared utility functions
```

- A module has only the subfolders it needs (`api`, `components`, `hooks`, `utils`).
- Modules never import from other modules. Compose them in `app/`; shared code goes in
  `components/`, `lib/`, `types/` or `utils/`. ESLint `import/no-restricted-paths` enforces it
  once `modules/` exists.
- kebab-case file and folder names. `@/` imports for paths under `web/src`. No barrel files.

## State

- `useState` for independent state, `useReducer` when one action updates related state. Pass
  shared state through props.
- Navigable or shareable state lives in the URL (React Router).
- Server data lives in TanStack Query, never copied into component state.

## API and live output

- One fetch-based client in `web/src/lib/api-client.ts`.
- Each request is declared once, beside its query or mutation hook in the module's `api/` folder,
  with an explicit response type.
- Query keys name the resource plus identifying inputs: `['threads', botId]`. Mutations invalidate
  the matching keys.
- Live run output arrives over Server-Sent Events (`EventSource`); user messages go out as HTTP
  `POST`.

## Checks

- `make check` runs Prettier, ESLint and `tsc` for `web/`.
- Vitest for logic that is not UI, once there is some. Prove UI behaviour by driving the real app.
