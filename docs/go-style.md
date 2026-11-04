# Go style

Condensed from the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md).
Where Uber and this file differ, this file wins. `make check` enforces formatting, imports and
lint (`.golangci.yml`); this file covers what the tools cannot.

## Design

- Standard library first. No framework until a ticket proves the need.
- No mutable globals. Inject dependencies through constructors. Avoid `init()`.
- Unexported package-level vars and consts start with `_` (`_defaultTimeout`).
- Do not embed types in exported structs; it leaks the inner API.
- Verify interface compliance at compile time: `var _ Provider = (*Claude)(nil)`.
- Pass interfaces by value, never as pointers. One receiver kind per type.
- `sync.Mutex` as an unexported field, zero value, never a pointer.
- Copy slices and maps at boundaries: when you store one you received, and when you return
  internal state.
- Channel size is 1 or none. Anything else needs a written reason.
- Enums start at `iota + 1` unless the zero value is a meaningful default.
- Every struct encoded to JSON or another format has explicit tags on every field.
- Functional options only once a constructor has more than three optional settings. Until then, a
  config struct.

## Errors

- `errors.New` for static messages, `fmt.Errorf` for dynamic ones. A custom `XError` type when
  callers need fields; an exported `ErrX` sentinel when callers need `errors.Is`.
- Wrap with `%w` and add context the caller lacks, without "failed to": `fmt.Errorf("open store: %w", err)`.
- Handle each error once: log it or return it, never both.
- Two-value type assertions only: `v, ok := x.(T)`.
- No panics in library code. `main` calls one `run() error` and exits once on failure.

## Concurrency and time

- No fire-and-forget goroutines. Each one can be stopped (context or done channel) and waited for
  (`sync.WaitGroup` or `errgroup`).
- Typed atomics (`atomic.Int64`), never `atomic.AddInt64` on a plain int.
- `time.Time` for instants, `time.Duration` for periods, never `int` seconds. On the wire: RFC 3339
  for instants, explicit units in the name for periods (`timeoutMs`).

## Style

- Lines around 99 columns. Match the surrounding code before being right in the abstract.
- Package names: short, lowercase, one word. Not `util`, `common`, `helpers`.
- In a file: types, constructors, methods by receiver, exported before unexported, plain helpers last.
- Handle the error or edge case first and return. No `else` after a return.
- Smallest scope: `if err := f(); err != nil {`. `:=` by default; `var x T` for a wanted zero value.
- `nil` is the empty slice; check with `len(s) == 0`.
- Structs initialised with field names, zero fields omitted, `&T{}` not `new(T)`.
- No unclear bare arguments (`f(true, false)`); use named constants or a small struct.
- Raw string literals for text with quotes or backslashes.
- Never shadow built-ins (`error`, `len`, `new`, `copy`).
- `strconv` over `fmt` for numbers. Give `make` a capacity when you know it.

## Tests

- Test behaviour the way a caller sees it, with literal expected values. Few tests, each earning
  its place. No code that exists only for a test.
- Table tests: `tests := []struct{ name string; give X; want Y }` and `t.Run(tt.name, ...)`. One
  assertion idea per row. Different setup or control flow means separate tests, not flags.
- Test names may use underscores to separate the case: `TestStore_Create`.
