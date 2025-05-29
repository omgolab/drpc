
# Standard Development Rules

<standard_rules>

## Go/golang Rules

- **API/Type Troubleshooting:** Use `go doc <pkg> <optional_type>` to find interface definitions or troubleshoot API/type errors.
- **Go Version:** Latest is 1.24.
- **Test Organization:**
  - Integration tests: Use `_integration_test.go` suffix in `package mypackage_test`
  - Unit tests: Same package (e.g., `package mypackage`)
  - Mocks: Place in dedicated `mocks` folder with `_mocks.go` suffix
- **Testing Practices:**
  - Run tests with `go test -count=1 ./...` to avoid caching
  - Use mocks for external dependencies in unit tests
  - Create integration tests with real implementations
- **Code Quality:**
  - Always validate parameters, check pointers for nil
  - For container-based tests, ensure proper cleanup with defer statements
  - Use named volumes instead of host path mounts for cross-platform compatibility
- **Architecture:** `internal` can depend on `pkg`, but not vice versa. Consider `pkg` as a standalone library.

## TS/Typescript Rules

- **Package Management:** Use `bun` as package manager. For external commands, install packages locally with `bun i -D {pkg_cmd}` then run with `bun {pkg_cmd}` instead of using `bunx/pnpx/npx`.
- However, to run TS script use: tsx {script.ts} 

## Testing Best Practices

- **Approach:**
  - With existing code: Create tests based on existing code
  - Without source code: Follow TDD approach
  - Prefer real environments over mocks when possible
  - Address failed tests one file at a time
- **Debugging:**
  - Always re-read source files when tests fail
  - Ensure sequential logging strategies even in parallel tests
  - Validate test data availability before execution
  - Provide all required parameters in test calls
- **Playwright Specific:**
  - Development command: `bun playwright test e2e --project=chromium --reporter=list`
  - Enable `trace: 'retain-on-failure'` and set `fullyParallel: false`
  - Pipe browser console logs to test output
  - For timeouts, print inner HTML of elements being interacted with
  - Add event listeners for 'request', 'response', and 'console' events
  - Avoid `--debug` flag during automation

## General Development Rules

- **Code Management:**
  - Don't modify generated files; update their source configurations instead
  - Check for linting issues after each file modification
  - Always split code file into as many file as possible by role; rather than having a large file
- **Frontend:**
  - Prefer existing HMR over starting new development servers; but if we must a new start - stop existing ports before a restart
- **Architecture:**
  - Always prefer reactive system design for more efficient resource use

</standard_rules>
