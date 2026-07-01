// server/handler/codegen/setup_test.go — Shared helpers for the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// codegen handler integration tests.
//
// Why this file exists:
//
//	The four handlers in this package (submit, status, stream, cancel)
//	read from and write to a *redis.Client; submit also enqueues an
//	Asynq task via *asynq.Client. Spinning up real Redis for tests is
//	brittle and CI-hostile, so we use miniredis — an in-process
//	Redis-compatible server that both go-redis and asynq talk to over
//	the loopback. Each test gets its own miniredis instance, scoped by
//	t.Cleanup, so no test ever sees state from another.
//
//	Every handler now also gates on the caller's JWT identity (see
//	ownership.go). Production wires that via spaauth.RequireBearerToken;
//	these tests bypass JWT parsing entirely by mounting a stub middleware
//	(injectTestClaims) that puts a chosen userID into the same context
//	slot RequireBearerToken would. The result: handler-level tests
//	exercise the real ownership logic against a deterministic identity
//	without minting tokens.
//
//	The asynq.Client points at the SAME miniredis as the *redis.Client.
//	Tests that want to verify a task was enqueued create an asynq
//	Inspector against the same address (helper newInspector below).
//	No worker is started — the tests check the side effects on Redis
//	directly, never end-to-end completion.
//
// External dependency:
//
//	github.com/alicebob/miniredis/v2 — the in-process Redis fake.
//	github.com/hibiken/asynq        — already a production dep.
//	Add miniredis with `go get -t github.com/alicebob/miniredis/v2`
//	before running these tests. It's a test-only dependency; production
//	binaries do not link it.
package codegen

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	cryptoauth "server/auth"
	"server/handler/spaauth"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// defaultTestUserID is the identity used by newCodegenTestServer. Tests
// that need to exercise cross-tenant behaviour use
// newCodegenTestServerForUser with an alternative ID and seed Redis keys
// to belong to defaultTestUserID — the mismatch is the property under
// test.
//
// Português:
//
//	Identidade padrão usada pelos testes que não se preocupam com
//	cross-tenant. Para cross-tenant, ver newCodegenTestServerForUser.
const defaultTestUserID = "test-user-1"

// newCodegenTestServer builds an Echo app with the four codegen routes
// mounted, backed by a fresh miniredis instance and a stub middleware
// that injects defaultTestUserID as the authenticated user.
//
// Returns the echo handler (for ServeHTTP), the miniredis (so tests can
// poke or inspect Redis state directly), and the handler struct (in
// case a test wants to call methods directly without HTTP).
//
// The real spaauth.RequireBearerToken middleware is NOT mounted here:
// it would require a JWT secret, a real token, and a header on every
// request. The stub middleware reaches the same end (claims in the
// context) without any of that ceremony. The ownership logic itself
// is exercised — only the JWT parsing is short-circuited.
func newCodegenTestServer(t *testing.T) (*echo.Echo, *miniredis.Miniredis, *handler) {
	return newCodegenTestServerForUser(t, defaultTestUserID)
}

// newCodegenTestServerForUser is the same as newCodegenTestServer but
// lets the caller choose the userID the stub middleware injects. Used
// by cross-tenant tests: they call this with userID="attacker", seed
// Redis as if defaultTestUserID owned the job, and assert the response
// matches the not-found shape.
func newCodegenTestServerForUser(
	t *testing.T,
	userID string,
) (*echo.Echo, *miniredis.Miniredis, *handler) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	// The Asynq client uses its own connection pool against the same
	// miniredis. Asynq stores its queues in well-known Redis keys
	// (asynq:{<queue>}:pending, etc.) which the Inspector below reads.
	redisOpt := asynq.RedisClientOpt{Addr: mr.Addr()}
	ac := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = ac.Close() })

	// The Inspector is what the cancel endpoint (and the stream
	// disconnect path) uses to ask Asynq to abort an in-flight task.
	// We share its RedisClientOpt with the client so they observe the
	// same queue.
	insp := asynq.NewInspector(redisOpt)
	t.Cleanup(func() { _ = insp.Close() })

	h := &handler{redis: rdb, asynq: ac, inspector: insp}

	e := echo.New()
	g := e.Group("/api/v1/codegen", injectTestClaims(userID))
	g.POST("/:language", h.handleSubmit)
	g.GET("/jobs/:id/status", h.handleStatus)
	g.GET("/jobs/:id/stream", h.handleStream)
	g.POST("/jobs/:id/cancel", h.handleCancel)

	return e, mr, h
}

// injectTestClaims returns an echo middleware that pretends the Bearer
// validation has already happened and that the JWT carried userID.
// Production code MUST NOT use this — the corresponding pipeline is
// spaauth.RequireBearerToken which actually verifies a signed token.
//
// If userID is empty, the middleware writes a claims object with an
// empty UserID, which mirrors the production behaviour when the Bearer
// middleware is bypassed entirely (and lets us test how the handlers
// react to a missing identity).
//
// Português:
//
//	Middleware de teste que injeta claims diretamente no contexto,
//	pulando a parse do JWT. Substitui spaauth.RequireBearerToken
//	apenas em testes.
func injectTestClaims(userID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			spaauth.SetClaims(c, &cryptoauth.Claims{UserID: userID})
			return next(c)
		}
	}
}

// newInspector returns an asynq.Inspector pointed at the same miniredis
// the test server is using. Used by submit tests to verify that a task
// was placed on the codegen queue without standing up a real worker.
//
// The inspector is registered with t.Cleanup so the caller does not
// have to track its lifecycle.
func newInspector(t *testing.T, mr *miniredis.Miniredis) *asynq.Inspector {
	t.Helper()
	insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: mr.Addr()})
	t.Cleanup(func() { _ = insp.Close() })
	return insp
}

// emptyScene is the smallest scene JSON that flows through codegen
// without errors: zero devices, zero wires, valid metadata block.
// Suitable for tests that don't care about the generated code, only
// about the handler's request/response shape.
const emptyScene = `{
	"version": "1",
	"metadata": {
		"density": 1,
		"canvasWidth": 800,
		"canvasHeight": 600,
		"camera": {"offsetX": 0, "offsetY": 0, "zoom": 1}
	},
	"devices": [],
	"wires": []
}`

// jsonRequest builds an HTTP request with a JSON body. No Authorization
// header is set — the test middleware (injectTestClaims) provides
// identity instead.
func jsonRequest(t *testing.T, method, path string, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

// readAll is a tiny convenience for slurping a response body in tests.
func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	return string(b)
}

// decodeJSON unmarshals JSON into out, failing the test on error.
func decodeJSON(t *testing.T, raw string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("decodeJSON: %v. raw: %s", err, raw)
	}
}
