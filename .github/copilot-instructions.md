# Coding Agent Instructions

Follow the repository-wide instructions in `../AGENTS.md`.

Important shared slash directives:

- `/dev`: use development-stage implementation rules and do not add backward compatibility or legacy support unless explicitly requested.
- `/no-test`: do not write or run tests for the active task or session.
- `/docu`: update project documentation for code changes made in the active task or session.

Recognize wrapped forms such as `-/dev-`, `-/no-test-`, and `-/docu-` as aliases for the same directives.
