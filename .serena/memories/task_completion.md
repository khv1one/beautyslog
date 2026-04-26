# Task Completion Checklist

When a task is completed:
1. Ensure all code builds.
2. Verify tests pass: `go test -race ./...`.
3. Check test coverage.
4. Ensure functionality aligns with the approved plan.
5. Review changes for architecture, performance (leaks, races), and consistency.
6. If applicable, run `golangci-lint`.
7. Update relevant documentation.
8. Mark the task as completed in the todo list.
