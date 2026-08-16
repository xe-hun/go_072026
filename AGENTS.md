# Coding Agent Instructions

These instructions apply to the whole repository unless a more specific instruction file overrides them.

## Shared Slash Directives

Treat the following as session-scoped user directives. They may appear anywhere in a user message before or alongside the task. Recognize both bare forms such as `/dev` and wrapped forms such as `-/dev-`; the wrapping hyphens are optional markers, not part of the command name.

When a directive is active, keep following it for the current coding session until the user explicitly disables it or gives a newer conflicting instruction. More specific user instructions and higher-priority system or safety instructions still take precedence.

### `/dev`

Use development-stage implementation rules.

- Add or fix the requested feature directly in the current code.
- Do not add backward compatibility, legacy support, compatibility aliases, migration shims, duplicate old code paths, deprecation layers, or fallback behavior only meant to preserve old callers.
- It is acceptable to make breaking changes to local APIs, schemas, configuration, routes, generated code, docs, and tests when those changes are the direct result of the requested task.
- Still preserve correctness, security, data integrity, secrets hygiene, and unrelated user work. Development-stage mode removes legacy compatibility constraints; it does not permit careless destructive edits.

### `/no-test`

Skip test authoring and test execution for the active task or session.

- Do not create, update, or regenerate test files for the requested work.
- Do not run test commands, test suites, snapshot tests, integration tests, end-to-end tests, or coverage commands.
- Non-test checks such as formatting, code generation, or a build may still be used when needed, unless the user also asks to skip those.
- In the final response, clearly state that tests were not written or run because `/no-test` was active.

### `/docu`

Document the code changes made during the active task or session.

- Update an existing documentation file when one already fits the change, such as `README.md`, `documentation.md`, `run-instruction.md`, or another project-specific document.
- Add a new documentation file only when no existing document is appropriate.
- Document user-visible behavior, setup or command changes, API/schema/configuration changes, and any intentional breaking changes, especially when `/dev` is also active.
- Keep documentation concise and aligned with the repository's existing style.

## Compatibility Guidance

These slash directives are plain-text agent instructions. Agents with native slash-command support may register equivalent commands, but agents without that support must still parse and honor the tokens from normal user messages.

Do not copy directive tokens into source code, comments, commit messages, generated output, or documentation unless the task is specifically to describe the directives themselves.
