# Test Fixes: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser` | **Fixed**: 2026-10-09 | **Review source**: test-review.md (2026-10-09)

## Summary

- Findings processed: 4 / 4
- Mechanical fixes applied: 4 (R1–R4)
- Escalated and resolved: 0
- Skipped: 0
- Independent correction review: PENDING
- Re-review scope: CFG-001, CFG-002, HTTP-003, SEC-001

## Mechanical Fixes

### R1 — CFG-001: Nonzero normal process exit

**File**: `cmd/service/authentication_test.go:232`

**Current assertions**: The real service process must exit normally with any nonzero status. Listener exclusion and sanitized setting diagnostics remain required.


Before:
```go
			if assert.ErrorAs(t, runErr, &exit, tc.name) {
				assert.Equal(t, 1, exit.ExitCode(), tc.name)
			}
```

After:
```go
			if assert.ErrorAs(t, runErr, &exit, tc.name) {
				assert.True(t, exit.Exited(), "must exit normally rather than be killed: %s", tc.name)
				assert.NotZero(t, exit.ExitCode(), tc.name)
			}
```

### R2 — CFG-002: Actual migrate database failure

**File**: `cmd/service/authentication_test.go:311`

**Current assertions**: Both absent and invalid Basic settings exercise the actual migrate command against a test-owned endpoint that accepts TCP and closes without a PostgreSQL handshake. The test requires an observed connection, a bounded database error, no success log, and sanitized failure output. The endpoint and goroutine have a cleanup path. The valid PostgreSQL success and common runtime-validation witnesses remain required.


Before:
```go
		assert.NoError(t, run(context.Background(), []string{"migrate"}, env.get, &output), basic) // V4
		assert.Contains(t, output.String(), "migrations complete")
		var ready int
		assert.NoError(t, pool.QueryRow(context.Background(), "SELECT 1").Scan(&ready))
		assert.Equal(t, 1, ready)
```

After:
```go
		assert.NoError(t, run(context.Background(), []string{"migrate"}, env.get, &output), basic) // V4
		assert.Contains(t, output.String(), "migrations complete")
		var ready int
		assert.NoError(t, pool.QueryRow(context.Background(), "SELECT 1").Scan(&ready))
		assert.Equal(t, 1, ready)
		// A live test-owned TCP endpoint accepts the database connection but
		// cannot speak PostgreSQL. A no-op migrate command cannot pass this
		// witness: both a real connection attempt and a bounded failure are required.
		unavailable, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		var closeUnavailable sync.Once
		closeEndpoint := func() { closeUnavailable.Do(func() { assert.NoError(t, unavailable.Close()) }) }
		t.Cleanup(closeEndpoint)
		attempted := make(chan error, 1)
		go func() {
			conn, acceptErr := unavailable.Accept()
			if acceptErr == nil {
				acceptErr = conn.Close()
			}
			attempted <- acceptErr
		}()
		originalURL := values["DATABASE_URL"]
		values["DATABASE_URL"] = "postgres://authentication:unavailable-database-secret@" + unavailable.Addr().String() + "/authentication?sslmode=disable"
		outputBeforeFailure := output.Len()
		migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 2*time.Second)
		start = time.Now()
		err = run(migrationCtx, []string{"migrate"}, env.get, &output)
		cancelMigration()
		assert.Less(t, time.Since(start), 2*time.Second, basic)
		if assert.Error(t, err, basic) {
			assert.Contains(t, strings.ToLower(err.Error()), "database", basic)
			assert.NotContains(t, err.Error(), "BASIC_AUTH", basic)
			assert.NotContains(t, err.Error(), "unavailable-database-secret", basic)
		}
		assert.NotContains(t, output.String()[outputBeforeFailure:], "migrations complete", basic)
		select {
		case acceptErr := <-attempted:
			assert.NoError(t, acceptErr, "migrate must access the database: %s", basic)
		case <-time.After(time.Second):
			// Closing the listener also completes the owned goroutine when the
			// command never attempted a connection.
			closeEndpoint()
			<-attempted
			t.Errorf("migrate never connected to the database: %s", basic)
		}
		values["DATABASE_URL"] = originalURL
```


Production `db/migrations/` contains only `.gitkeep`. The success witness covers empty-source migration execution; helper connectivity is not evidence of command-produced migration effects. Nonempty migration effects require assertions when production migrations exist.

### R3 — HTTP-003: Bounded inspection without a one-byte algorithm

**File**: `internal/web/authentication_test.go:506`

**Current assertions**: Inspection must read actual input and consume less than the 1 MiB fixture. Finite buffers such as 512 bytes are allowed. Declared-length independence, empty EOF, nonempty rejection, GET/HEAD behavior, read errors, and real stalled-stream witnesses remain in the suite.


Before:
```go
			if body, ok := tc.reader.(*observedBody); ok {
				assert.Positive(t, body.reads.Load(), tc.name)
				assert.LessOrEqual(t, body.bytes.Load(), int32(1), "landing must inspect at most one byte: %s", tc.name)
			}
```

After:
```go
			if body, ok := tc.reader.(*observedBody); ok {
				assert.Positive(t, body.reads.Load(), tc.name)
				// The large fixture must not be consumed in full. This allows
				// finite inspection buffers without prescribing their size.
				assert.Less(t, body.bytes.Load(), int32(1<<20), "landing must bound actual-byte inspection: %s", tc.name)
			}
```

### R4 — CFG-001, SEC-001: Submitted tokens and decoded diagnostic values

**File**: `cmd/service/authentication_test.go:243`; `internal/web/authentication_test.go:622`

**Current assertions**: Startup diagnostics are checked both as raw text and decoded JSON values, including JSON-normalized invalid UTF-8 credential strings. Each submitted Authorization value contributes its full header, encoded token, decoded pair, username, and password to forbidden values. Response bodies/headers, raw logs, and decoded structured attributes are inspected. Generated correlation, exact outcome status, and duration assertions remain required.


Startup diagnostic block


Before:
```go
		diagnostic := logs.contents()
		if runErr != nil {
			diagnostic += runErr.Error()
		}
		for _, secret := range []string{tc.username, tc.password, "database-fixture-secret"} {
			if secret != "" {
				assert.NotContains(t, diagnostic, secret, tc.name)
			} // V9
		}
		assert.NotContains(t, diagnostic, base64.StdEncoding.EncodeToString([]byte(tc.username+":"+tc.password)), tc.name)
```

After:
```go
		diagnostic := logs.contents()
		// Inspect decoded structured values so JSON escaping cannot conceal a
		// credential containing controls or other escaped characters.
		for _, line := range strings.Split(strings.TrimSpace(logs.contents()), "\n") {
			var entry map[string]any
			if assert.NoError(t, json.Unmarshal([]byte(line), &entry), tc.name) {
				diagnostic += "\n" + fmt.Sprint(entry)
			}
		}
		if runErr != nil {
			diagnostic += runErr.Error()
		}
		for _, secret := range []string{tc.username, tc.password, "database-fixture-secret"} {
			if secret != "" {
				assert.NotContains(t, diagnostic, secret, tc.name)
				// encoding/json replaces invalid UTF-8 in string values. Check
				// that representation too, without losing the raw-byte check.
				encoded, err := json.Marshal(secret)
				require.NoError(t, err)
				var normalized string
				require.NoError(t, json.Unmarshal(encoded, &normalized))
				assert.NotContains(t, diagnostic, normalized, tc.name)
			} // V9
		}
		assert.NotContains(t, diagnostic, base64.StdEncoding.EncodeToString([]byte(tc.username+":"+tc.password)), tc.name)
```


Request sanitization and structured outcome block


Before:
```go
		combined := logs.String() + w.Body.String() + fmt.Sprint(w.Header())
		for _, secret := range []string{authUser, authPassword, base64.StdEncoding.EncodeToString([]byte(authUser + ":" + authPassword)), "database-secret", "submitted-wrong-secret", "malformed-authorization-secret", "sensitive-body-sentinel", "sensitive-read-error-sentinel", "query-secret-sentinel", "untrusted-request-id-secret"} {
			assert.NotContains(t, combined, secret, tc.name)
		}
		var completed bool
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var entry map[string]any
			if !assert.NoError(t, json.Unmarshal([]byte(line), &entry), tc.name) {
				continue
			}
			if entry["msg"] == "request completed" {
				completed = true
				assert.Equal(t, id, entry["request_id"])
				assert.EqualValues(t, tc.status, entry["status"])
				assert.Contains(t, entry, "duration_ms")
			}
		}
		assert.True(t, completed, "missing structured completion outcome: %s", tc.name)
```

After:
```go
		combined := logs.String() + w.Body.String() + fmt.Sprint(w.Header())
		forbidden := []string{authUser, authPassword, base64.StdEncoding.EncodeToString([]byte(authUser + ":" + authPassword)), "database-secret", "submitted-wrong-secret", "malformed-authorization-secret", "sensitive-body-sentinel", "sensitive-read-error-sentinel", "query-secret-sentinel", "untrusted-request-id-secret"}
		for _, header := range tc.headers {
			forbidden = append(forbidden, header)
			parts := strings.Fields(header)
			if len(parts) == 2 {
				forbidden = append(forbidden, parts[1])
				if decoded, err := base64.StdEncoding.DecodeString(parts[1]); err == nil {
					forbidden = append(forbidden, string(decoded))
					if username, password, ok := strings.Cut(string(decoded), ":"); ok {
						forbidden = append(forbidden, username, password)
					}
				}
			}
		}
		var completed bool
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var entry map[string]any
			if !assert.NoError(t, json.Unmarshal([]byte(line), &entry), tc.name) {
				continue
			}
			// Include decoded structured values, including nested attributes,
			// rather than inspecting only their escaped JSON serialization.
			combined += "\n" + fmt.Sprint(entry)
			if entry["msg"] == "request completed" {
				completed = true
				assert.Equal(t, id, entry["request_id"])
				assert.EqualValues(t, tc.status, entry["status"])
				assert.Contains(t, entry, "duration_ms")
			}
		}
		for _, secret := range forbidden {
			assert.NotContains(t, combined, secret, tc.name)
		}
		assert.True(t, completed, "missing structured completion outcome: %s", tc.name)
```


## Resolved Escalations

None. All fixes are confined to their flagged test files and planned named tests. No shared helper or production declaration is modified.

## Skipped

None.

## Validation

- Snapshot: post-test overwritten; comparison against baseline — PASS. Complete inventory: 57 tests, 42 passed, 15 intentionally failed, 0 pending; 38 existing outcomes preserved; all 19 new outcomes conform to the plan.
- `banned-patterns.sh cmd/service/authentication_test.go internal/web/authentication_test.go` — PASS; seven test files inspected by the Go runner; RSpec matcher bans are inapplicable.
- Allowlist: 15 approved plan entries; 0 appended in this session.
- The restricted capture cannot execute the complete integration inventory; the successful capture uses local HTTP/TCP listeners and PostgreSQL 18.6 test containers outside the sandbox, with a temporary writable Go build cache.
- `gofmt -l` for both modified test files and `git diff --check` — PASS.
- `.tools/bin/golangci-lint run --timeout=5m ./cmd/service/... ./internal/web/...` — PASS; 0 issues.
- `go test -race -count=1 -timeout=3m ./cmd/service ./internal/web -run '^(TestAuthenticationServingConfiguration|TestAuthenticationCommandIndependence|TestAuthenticationLandingBodyBytes|TestAuthenticationSanitizedOutcomes)$'` — nonzero for the three intentionally red authentication tests; command independence passes. No data-race diagnostics. This is not a green race-suite result.
- Aggregate `make verify` and vulnerability checking are not run by this correction session. Full green verification remains an implementation delivery obligation.
- `session-lock.sh check-dirty test` — PASS.
- Independent scoped correction review — PENDING. The existing `test-review.md` is preserved; mechanical gates do not establish readiness.

## Review Scope and Coverage

Changed test paths:

- `cmd/service/authentication_test.go`: `TestAuthenticationServingConfiguration` (CFG-001, SEC-001 V1) and `TestAuthenticationCommandIndependence` (CFG-002).
- `internal/web/authentication_test.go`: `TestAuthenticationLandingBodyBytes` (HTTP-003) and `TestAuthenticationSanitizedOutcomes` (SEC-001).

No registrations, identities, criterion mappings, test organization, planned landings, or upstream contracts are altered. Snapshot metadata is regenerated exclusively by the required runner commands. No manual plan, snapshot, production, or review edits are made.

Invoke `/kaba:review-tests CFG-001 CFG-002 HTTP-003 SEC-001` in an isolated reviewer context. The reviewer must merge scoped findings, retain unaffected findings, and recompute the full verdict. Human final acceptance remains required after that independent review.
