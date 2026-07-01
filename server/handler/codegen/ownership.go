// server/handler/codegen/ownership.go — Per-user codegen job isolation.
//
// Why this file exists:
//
//	Codegen jobs are addressed by a random opaque jobID that travels in
//	URL paths (status / stream / cancel). Before this revision, ANY
//	authenticated user who learned a jobID — by guessing (impractical),
//	by seeing it in shared logs, or by shoulder-surfing — could read or
//	cancel the job. The Bearer middleware only proved the caller had AN
//	account; it did not prove the caller owned THIS job.
//
//	The fix is the smallest one that closes the gap: at submit time, the
//	handler writes codegen:job:{jobID}:userId alongside the other job
//	keys; every read/write endpoint compares that value against the JWT
//	claim before acting.
//
// Response policy on mismatch — the deliberate choice is "404 for every
// failure path that touches identity":
//
//	The project owner explicitly rejected 403 in favour of returning the
//	exact same response shape that a NON-EXISTENT job would have produced.
//	Rationale: a 403 leaks "this jobID exists, you just can't see it";
//	a 404 leaks nothing. The cost of that policy is harder ops debugging
//	(was it really missing, or was it someone else's?) — accepted by the
//	project owner.
//
//	Each endpoint maps the mismatch to its own shape-equivalent of "not
//	found":
//	  cancel  → HTTP 404 {error: "job not found or already expired"}
//	  status  → HTTP 200 {status: "unknown"}      (matches the redis.Nil branch)
//	  stream  → SSE event: error "job not found or expired"  (matches redis.Nil)
//
// Português:
//
//	Verificação de propriedade de job. O handler de submit grava o userID
//	do JWT em codegen:job:{jobID}:userId; status, stream e cancel
//	verificam essa chave e respondem como se o job não existisse quando
//	o userID do JWT não bate. Decisão explícita: nunca devolver 403, que
//	vazaria a existência do jobID.
package codegen

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// errEmptyUserID is returned by ownsJob when the caller has not been
// authenticated. In production the Bearer middleware guarantees a
// non-empty UserID before any codegen handler runs, so this can only
// fire if the routing is misconfigured — log it and treat the request
// as "no access".
//
// We declare this as a typed error rather than using errors.New inline
// so callers can distinguish "couldn't check ownership because the
// caller has no identity" from "couldn't check ownership because Redis
// is unhealthy". Today every caller treats both the same way (deny),
// but the distinction matters for log triage.
var errEmptyUserID = errors.New("codegen: empty userID in claims")

// ownsJob reports whether userID matches the owner stored in Redis for
// the given jobID.
//
// Return contract:
//
//	owns=true, err=nil   — the caller is the owner; proceed with the action.
//	owns=false, err=nil  — the job either does not exist or belongs to
//	                       someone else. Caller MUST respond with the
//	                       "not found" shape for the relevant endpoint.
//	owns=false, err!=nil — infra error reaching Redis, OR the caller had
//	                       an empty userID. Caller MUST still respond
//	                       with the "not found" shape — we never want a
//	                       500 path here to leak that a jobID exists.
//
// The "err returned but answer is still deny" pattern is deliberate:
// the action this method gates is access to a resource, and from the
// attacker's perspective a 500 is identical to a 404 — both are not-200.
// Letting infra failures cause 500s instead of 404s would let an
// attacker correlate latency or error codes with jobID existence.
//
// The function does NOT log on its own; the callers do, with their own
// feature tag (status / stream / cancel) so log greppability is preserved.
//
// Português:
//
//	Verifica se o userID do JWT bate com o dono gravado em
//	codegen:job:{jobID}:userId. Retorno (false, nil) cobre tanto job
//	inexistente quanto job de outro usuário — o handler chamador
//	devolve a mesma resposta nos dois casos. Erros de infra também
//	resultam em deny: nunca queremos vazar existência do jobID por
//	via de erro 500.
func (h *handler) ownsJob(ctx context.Context, jobID, userID string) (owns bool, err error) {
	// An empty userID means the auth middleware did not populate claims
	// — either the middleware was bypassed (configuration bug) or the
	// JWT itself carried an empty subject (token-minting bug). Either
	// way, treat as "no access".
	if userID == "" {
		return false, errEmptyUserID
	}

	key := fmt.Sprintf("codegen:job:%s:userId", jobID)
	owner, err := h.redis.Get(ctx, key).Result()
	switch {
	case errors.Is(err, redis.Nil):
		// No owner key in Redis. Two ways to get here:
		//   1. The jobID was never created.
		//   2. The job's TTL elapsed and Redis evicted the key.
		// From the caller's point of view both are "job is gone".
		return false, nil
	case err != nil:
		// Genuine infra error talking to Redis. Returned to the caller
		// so it can log appropriately, but the boolean is still false:
		// we never grant access on a Redis hiccup.
		return false, err
	}

	return owner == userID, nil
}
