# Contributing

## Language policy

| What | Language |
| --- | --- |
| Go identifiers, comments, doc comments | English |
| Log messages and log fields | English |
| Commit messages, branch names | English |
| Web UI copy, dialogs, toasts, in-app error popups | Chinese |
| Code review notes, issue text | English |

Rule of thumb: **anything a developer reads is English, anything an end user reads is Chinese.**

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/), one logical change per commit.

```
<type>(<scope>): <summary>
```

- `type`: `feat` `fix` `refactor` `perf` `test` `docs` `chore` `build` `ci` `style`
- `summary`: imperative mood, lowercase, no trailing period, <= 72 chars
- body (optional): explain **why**, not what; wrap at 72 columns
- breaking change: `feat(protocol)!: drop v1 handshake` plus a `BREAKING CHANGE:` footer

Examples:

```
feat(protocol): add handshake packet parser with varint tables
fix(tunnel): propagate half-close so server-list ping stops hanging
refactor(room): extract store interface behind room registry
test(mcproto): cover malformed varint lengths
chore: bootstrap repo with contributing guide
```

## Commit granularity

One small, self-contained piece of work per commit. A commit should build and pass
tests on its own. Do not bundle an unrelated cleanup with a feature.

## Git workflow

- **Never run `git push`.** Pushing is done manually by the maintainer.
- **Never modify remotes** (`git remote add/set-url/remove`).
- Committing locally is expected after each completed piece of work.

## Definition of done (per change)

1. `gofmt -l .` prints nothing.
2. `go vet ./...` is clean.
3. `go test ./...` passes.
4. New core logic (packet parsing, routing, framing, config precedence) ships with unit tests.
5. Only then: `git commit`.
