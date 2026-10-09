package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const commandUser = "command-fixture-user"
const commandPassword = " command:fixture-password "

type commandOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *commandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

// Avoid String: the runner conservatively associates same-named methods with
// existing bytes.Buffer.String calls even when the concrete receiver differs.
func (b *commandOutput) contents() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

func commandDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := testcontainers.Run(ctx, "postgres:18.6-alpine",
		testcontainers.WithEnv(map[string]string{"POSTGRES_USER": "authentication", "POSTGRES_PASSWORD": "database-fixture-secret", "POSTGRES_DB": "authentication"}),
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)))
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	url := fmt.Sprintf("postgres://authentication:database-fixture-secret@%s/authentication?sslmode=disable", net.JoinHostPort(host, port.Port()))
	pool, err := postgres.Open(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, postgres.Migrate(ctx, url, os.DirFS("../../db/migrations"), "up"))
	return url, pool
}

func commandAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := ln.Addr().String()
	require.NoError(t, ln.Close())
	return address
}

func commandEnvironment(t *testing.T, databaseURL string) map[string]string {
	t.Helper()
	return map[string]string{
		"DATABASE_URL": databaseURL, "LISTEN_ADDR": commandAddress(t),
		"BASIC_AUTH_USERNAME": commandUser, "BASIC_AUTH_PASSWORD": commandPassword,
		"DB_STARTUP_TIMEOUT": "1s", "READINESS_TIMEOUT": "100ms", "SHUTDOWN_GRACE": "100ms", "CLEANUP_TIMEOUT": "1s",
	}
}

// Mutable environment access is synchronized so lifetime tests can change the
// source during service execution without introducing a test-owned data race.
type commandEnv struct {
	mu     sync.RWMutex
	values map[string]string
}

func (e *commandEnv) get(key string) string { e.mu.RLock(); defer e.mu.RUnlock(); return e.values[key] }
func (e *commandEnv) credentials(username, password string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.values["BASIC_AUTH_USERNAME"], e.values["BASIC_AUTH_PASSWORD"] = username, password
}

func startCommand(t *testing.T, env *commandEnv) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	logs := &commandOutput{}
	go func() { done <- run(ctx, []string{"serve"}, env.get, logs) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Error("serve did not finish within shutdown budget")
			}
		})
	}
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return strings.Contains(logs.contents(), "service listening") }, 3*time.Second, 5*time.Millisecond)
	return "http://" + env.get("LISTEN_ADDR"), stop
}

func commandResponse(t *testing.T, address, username, password string) (int, string, http.Header) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/", nil)
	require.NoError(t, err)
	if username != "" || password != "" {
		r.SetBasicAuth(username, password)
	}
	response, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	defer func() { assert.NoError(t, response.Body.Close()) }()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(data), response.Header
}

func TestAuthenticationServingConfiguration(t *testing.T) { // CFG-002 FND-002; authentication configuration and SEC-001
	url, _ := commandDatabase(t)
	binary := filepath.Join(t.TempDir(), "service")
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), time.Minute)
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, ".")
	buildOutput, buildErr := build.CombinedOutput()
	cancelBuild()
	require.NoError(t, buildErr, "build real service entry point: %s", buildOutput)
	for _, tc := range []struct {
		name, username, password, invalidSetting string
		missing                                  bool
	}{
		{"username_minimum", "!", commandPassword, "", false},                                                       // V1
		{"username_maximum", strings.Repeat("~", 128), commandPassword, "", false},                                  // V1
		{"username_empty", "", commandPassword, "BASIC_AUTH_USERNAME", false},                                       // V2
		{"username_overlong", strings.Repeat("U", 129), commandPassword, "BASIC_AUTH_USERNAME", false},              // V2
		{"password_minimum", commandUser, " ", "", false},                                                           // V3
		{"password_maximum", commandUser, strings.Repeat("~", 256), "", false},                                      // V3
		{"password_empty", commandUser, "", "BASIC_AUTH_PASSWORD", false},                                           // V4
		{"password_overlong", commandUser, strings.Repeat("P", 257), "BASIC_AUTH_PASSWORD", false},                  // V4
		{"username_missing", "", commandPassword, "BASIC_AUTH_USERNAME", true},                                      // V5
		{"password_missing", commandUser, "", "BASIC_AUTH_PASSWORD", true},                                          // V5
		{"username_space", "invalid username sentinel", commandPassword, "BASIC_AUTH_USERNAME", false},              // V6
		{"username_colon", "invalid:username-sentinel", commandPassword, "BASIC_AUTH_USERNAME", false},              // V6
		{"username_control", "invalid\tusername-sentinel", commandPassword, "BASIC_AUTH_USERNAME", false},           // V6
		{"username_non_ascii", "invalid-\xc3\xa9-username-sentinel", commandPassword, "BASIC_AUTH_USERNAME", false}, // V6
		{"username_delete", "invalid\x7fusername-sentinel", commandPassword, "BASIC_AUTH_USERNAME", false},          // V6
		{"password_control", commandUser, "invalid\npassword-sentinel", "BASIC_AUTH_PASSWORD", false},               // V7
		{"password_non_ascii", commandUser, "invalid-\xc3\xa9-password-sentinel", "BASIC_AUTH_PASSWORD", false},     // V7
		{"password_invalid_utf8", commandUser, "invalid-\xff-password-sentinel", "BASIC_AUTH_PASSWORD", false},      // V7
		{"password_delete", commandUser, "invalid\x7fpassword-sentinel", "BASIC_AUTH_PASSWORD", false},              // V7
		{"password_spaces_colon", commandUser, commandPassword, "", false},                                          // V8
	} {
		values := commandEnvironment(t, url)
		values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_test_fixture", "http://localhost:8080"
		values["BASIC_AUTH_USERNAME"], values["BASIC_AUTH_PASSWORD"] = tc.username, tc.password
		if tc.missing {
			delete(values, tc.invalidSetting)
		}
		env := &commandEnv{values: values}
		if tc.invalidSetting == "" {
			address, stop := startCommand(t, env)
			status, body, headers := commandResponse(t, address, tc.username, tc.password)
			assert.Equal(t, http.StatusOK, status, tc.name)
			assert.Contains(t, headers.Get("Content-Type"), "text/html", tc.name)
			assert.Contains(t, strings.ToLower(body), "payment", tc.name)
			status, _, _ = commandResponse(t, address, "", "")
			assert.Equal(t, http.StatusUnauthorized, status, tc.name)
			stop()
			continue
		}
		// This observation budget includes executable launch and suite scheduling;
		// it is a test hang guard, not a startup performance requirement.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
			case <-ctx.Done():
				// CommandContext kills overdue execution. Join the owned goroutine
				// and keep checking the result: a kill is not normal rejection.
				runErr = <-done
				t.Errorf("invalid serving configuration did not terminate within observation budget: %s", tc.name)
				break observeStartup
			}
		}
		observe.Stop()
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
	}
}

func TestAuthenticationCommandIndependence(t *testing.T) { // CFG-004; authentication command independence
	url, pool := commandDatabase(t)
	t.Chdir("../..")
	var notReady atomic.Bool
	var probeCalls atomic.Int32
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeCalls.Add(1)
		assert.Equal(t, "/readyz", r.URL.Path)
		assert.Empty(t, r.Header.Get("Authorization"))
		if notReady.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer probe.Close()
	for _, basic := range []string{"absent", "invalid"} {
		values := commandEnvironment(t, url)
		if basic == "absent" {
			delete(values, "BASIC_AUTH_USERNAME")
			delete(values, "BASIC_AUTH_PASSWORD")
			delete(values, "STRIPE_SECRET_KEY")
			delete(values, "APP_BASE_URL")
		} else {
			values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_live_invalid_sentinel", "https://example.com/invalid"
			values["BASIC_AUTH_USERNAME"] = "invalid username sentinel"
			values["BASIC_AUTH_PASSWORD"] = "invalid\tpassword-sentinel"
		}
		values["LISTEN_ADDR"] = strings.TrimPrefix(probe.URL, "http://")
		env := &commandEnv{values: values}
		var output bytes.Buffer
		notReady.Store(false)
		assert.NoError(t, run(context.Background(), []string{"probe"}, env.get, &output), basic) // V1
		notReady.Store(true)
		err := run(context.Background(), []string{"probe"}, env.get, &output)
		if assert.Error(t, err, basic) {
			assert.Contains(t, err.Error(), "not ready")
		} // V2
		values["LISTEN_ADDR"] = commandAddress(t)
		start := time.Now()
		err = run(context.Background(), []string{"probe"}, env.get, &output)
		assert.Error(t, err, basic)
		assert.Less(t, time.Since(start), 2*time.Second)                                           // V3
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
		for _, command := range []string{"probe", "migrate"} { // V5–V6
			for _, key := range []string{"DATABASE_URL", "HTTP_READ_TIMEOUT"} {
				original := values[key]
				if key == "DATABASE_URL" {
					values[key] = "postgres://user:runtime-secret@%gg"
				} else {
					values[key] = "0s"
				}
				err = run(context.Background(), []string{command}, env.get, &output)
				if assert.Error(t, err) {
					assert.Contains(t, err.Error(), key)
					assert.NotContains(t, err.Error(), "runtime-secret")
				}
				values[key] = original
			}
		}
		for _, secret := range []string{"invalid username sentinel", "invalid\tpassword-sentinel", "database-fixture-secret", "unavailable-database-secret"} {
			assert.NotContains(t, output.String(), secret)
		}
	}
	assert.EqualValues(t, 4, probeCalls.Load())
}

func TestAuthenticationCredentialLifetime(t *testing.T) { // CFG-002; authentication credential lifetime
	url, _ := commandDatabase(t)
	values := commandEnvironment(t, url)
	values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_test_fixture", "http://localhost:8080"
	env := &commandEnv{values: values}
	address, stop := startCommand(t, env)
	status, _, _ := commandResponse(t, address, commandUser, commandPassword)
	assert.Equal(t, http.StatusOK, status)
	env.credentials("rotated-fixture-user", "rotated-fixture-password")
	status, _, _ = commandResponse(t, address, commandUser, commandPassword)
	assert.Equal(t, http.StatusOK, status) // V1: original captured pair remains active.
	status, _, _ = commandResponse(t, address, "rotated-fixture-user", "rotated-fixture-password")
	assert.Equal(t, http.StatusUnauthorized, status)
	stop()
	address, stop = startCommand(t, env)
	status, _, _ = commandResponse(t, address, commandUser, commandPassword)
	assert.Equal(t, http.StatusUnauthorized, status) // V2: old pair fails after restart.
	status, body, _ := commandResponse(t, address, "rotated-fixture-user", "rotated-fixture-password")
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, strings.ToLower(body), "payment")
	stop()
}
