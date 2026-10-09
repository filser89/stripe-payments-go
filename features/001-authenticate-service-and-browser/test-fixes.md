# Test Fixes: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser` | **Fixed**: 2026-10-09 | **Review source**: test-review.md (2026-10-09)

## Summary

- Findings processed: 1 / 1
- Mechanical fixes applied: 1 (R5)
- Escalated and resolved: 0
- Skipped: 0
- Snapshot compare: PASS
- Banned pattern scan: PASS
- Independent re-review: PENDING

## Mechanical Fixes

### R5 — CFG-001: Observe silent startup listeners

**File**: `cmd/service/authentication_test.go:201`
**Status**: FIXED
**Current assertions**: Every invalid-credential startup variant probes its configured address every 5 ms independently of log contents, with each TCP attempt bounded to 20 ms. Any successful connection sets the listener-violation witness. Process execution is bounded to 500 ms and the observer has a 3 s termination guard. Connections are closed and observer timers are stopped. Sanitized setting-only diagnostics, absent success logging, and normal nonzero exit remain required.

Before:
```go
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		logs := &commandOutput{}
		process := exec.CommandContext(ctx, binary, "serve")
		for key, value := range values {
			process.Env = append(process.Env, key+"="+value)
		}
		process.Stdout, process.Stderr = logs, logs
		done := make(chan error, 1)
		go func() { done <- process.Run() }()
		// Watch real startup while run is active. A successful listener is a
		// violation even if the process later returns an error on shutdown.
		var accepted bool
		var runErr error
		observe := time.NewTicker(5 * time.Millisecond)
		startupBudget := time.NewTimer(3 * time.Second)
	observeStartup:
		for {
			select {
			case runErr = <-done:
				break observeStartup
			case <-observe.C:
				if strings.Contains(logs.contents(), "service listening") {
					conn, err := net.DialTimeout("tcp", values["LISTEN_ADDR"], 20*time.Millisecond)
					if err == nil {
						accepted = true
						assert.NoError(t, conn.Close())
					}
				}
			case <-startupBudget.C:
				cancel()
				t.Fatal("invalid serving configuration did not terminate")
			}
		}
		observe.Stop()
		startupBudget.Stop()
		cancel()
		assert.False(t, accepted, tc.name)
		assert.NotContains(t, logs.contents(), "service listening", tc.name)
		if assert.Error(t, runErr, tc.name) {
			var exit *exec.ExitError
			if assert.ErrorAs(t, runErr, &exit, tc.name) {
				assert.True(t, exit.Exited(), "must exit normally rather than be killed: %s", tc.name)
				assert.NotZero(t, exit.ExitCode(), tc.name)
			}
			assert.Contains(t, logs.contents(), tc.invalidSetting, tc.name)
			other := "BASIC_AUTH_USERNAME"
			if other == tc.invalidSetting {
				other = "BASIC_AUTH_PASSWORD"
			}
			assert.NotContains(t, logs.contents(), other, tc.name)
		}
		diagnostic := logs.contents()
		// Inspect decoded structured values so JSON escaping cannot conceal a
		// credential containing controls or other escaped characters.
		for _, line := range strings.Split(strings.TrimSpace(logs.contents()), "\n") {
			var entry map[string]any
			if assert.NoError(t, json.Unmarshal([]byte(line), &entry), tc.name) {
```

After:
```go
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		logs := &commandOutput{}
		process := exec.CommandContext(ctx, binary, "serve")
		for key, value := range values {
			process.Env = append(process.Env, key+"="+value)
		}
		process.Stdout, process.Stderr = logs, logs
		done := make(chan error, 1)
		go func() { done <- process.Run() }()
		// Probe throughout startup independently of logs: silent serving is
		// a violation even if the process later reports a credential error.
		var accepted bool
		var runErr error
		observe := time.NewTicker(5 * time.Millisecond)
		startupBudget := time.NewTimer(3 * time.Second)
	observeStartup:
		for {
			select {
			case runErr = <-done:
				break observeStartup
			case <-observe.C:
				conn, err := net.DialTimeout("tcp", values["LISTEN_ADDR"], 20*time.Millisecond)
				if err == nil {
					accepted = true
					assert.NoError(t, conn.Close())
				}
			case <-startupBudget.C:
				cancel()
				t.Fatal("invalid serving configuration did not terminate")
			}
		}
		observe.Stop()
		startupBudget.Stop()
		cancel()
		assert.False(t, accepted, tc.name)
		assert.NotContains(t, logs.contents(), "service listening", tc.name)
		if assert.Error(t, runErr, tc.name) {
			var exit *exec.ExitError
			if assert.ErrorAs(t, runErr, &exit, tc.name) {
				assert.True(t, exit.Exited(), "must exit normally rather than be killed: %s", tc.name)
				assert.NotZero(t, exit.ExitCode(), tc.name)
			}
			assert.Contains(t, logs.contents(), tc.invalidSetting, tc.name)
			other := "BASIC_AUTH_USERNAME"
			if other == tc.invalidSetting {
				other = "BASIC_AUTH_PASSWORD"
			}
			assert.NotContains(t, logs.contents(), other, tc.name)
		}
		diagnostic := logs.contents()
		// Inspect decoded structured values so JSON escaping cannot conceal a
		// credential containing controls or other escaped characters.
		for _, line := range strings.Split(strings.TrimSpace(logs.contents()), "\n") {
			var entry map[string]any
			if assert.NoError(t, json.Unmarshal([]byte(line), &entry), tc.name) {
```

The review's counterexample silently serves for 100 ms before returning a sanitized credential error. Its address is eligible for connection attempts throughout that interval. Polling is a bounded runtime witness, not a guarantee of detecting arbitrarily brief listeners. No counterexample mutation is executed in this session.

## Resolved Escalations

None. The correction is confined to the flagged named test; no helper or production declaration requires modification.

## Skipped

None.

## Validation

- `snapshot-tests.sh capture post-test`: complete inventory of 57 tests; 42 passed, 15 intentionally failed, 0 pending. Execution uses the configured `go test ./...` runner with Go 1.27.2 and real PostgreSQL 18.6 containers.
- `snapshot-tests.sh compare post-test`: PASS. All 19 new outcomes conform; all 38 baseline outcomes remain passed; 11 allowlisted content drifts conform.
- Allowlist: 15 plan entries, 0 appended in-session.
- `banned-patterns.sh cmd/service/authentication_test.go`: PASS; seven Go test files inspected; RSpec matcher bans are inapplicable.
- `gofmt -l cmd/service/authentication_test.go` and `git diff --check`: PASS.
- `.tools/bin/golangci-lint run --timeout=5m ./cmd/service/...`: PASS; 0 issues.
- `go test -race -count=1 -timeout=3m ./cmd/service -run '^TestAuthenticationServingConfiguration$'`: FAIL for expected missing authentication/landing behavior and invalid-credential serving. This is intentionally red feature evidence, not a passing race-suite result.
- `session-lock.sh check-dirty test`: PASS.
- Go-cache access and local Docker/network operations require sandbox escalation; the restricted environment cannot complete the runner inventory.
- Aggregate `make verify` and vulnerability checks are not run in this test correction session. Full green verification remains a feature delivery obligation.
- Independent correction review: PENDING. The existing `test-review.md` remains authoritative until an independent review recomputes the verdict.

## Review Scope and Coverage

Changed test path: `cmd/service/authentication_test.go`, named test `TestAuthenticationServingConfiguration`. R5 covers CFG-001; the same test retains SEC-001 V1 diagnostic assertions. Credential variants, markers, registrations, identities, planned landings, and organization remain intact. Snapshot metadata is generated exclusively by the required runner commands.

Invoke `/kaba:review-tests CFG-001 SEC-001` in an isolated reviewer context. The reviewer must merge scoped findings, retain unaffected findings, and recompute the full verdict. Human final acceptance follows that independent review.
