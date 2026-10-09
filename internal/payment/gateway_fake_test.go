package payment

import (
	"context"
	"errors"
	"sync"
	"time"
)

type policyGatewayCall struct {
	Method      string
	Snapshot    Snapshot
	SessionID   string
	Deadline    time.Time
	HasDeadline bool
}
type policyReply struct {
	evidence SessionEvidence
	err      error
	run      func(context.Context, Snapshot, string) (SessionEvidence, error)
}
type policyGateway struct {
	mu      sync.Mutex
	replies []policyReply
	calls   []policyGatewayCall
}

func newPolicyGateway(replies ...policyReply) *policyGateway { return &policyGateway{replies: replies} }
func (g *policyGateway) Calls() []policyGatewayCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := append([]policyGatewayCall(nil), g.calls...)
	for i := range out {
		out[i].Snapshot = clonePolicySnapshot(out[i].Snapshot)
	}
	return out
}
func (g *policyGateway) invoke(ctx context.Context, method string, s Snapshot, id string) (SessionEvidence, error) {
	deadline, has := ctx.Deadline()
	g.mu.Lock()
	g.calls = append(g.calls, policyGatewayCall{Method: method, Snapshot: clonePolicySnapshot(s), SessionID: id, Deadline: deadline, HasDeadline: has})
	var reply policyReply
	if len(g.replies) > 0 {
		reply = g.replies[0]
		g.replies = g.replies[1:]
	} else {
		reply.err = errors.New("unexpected gateway call")
	}
	g.mu.Unlock()
	if reply.run != nil {
		return reply.run(ctx, s, id)
	}
	e := reply.evidence
	e.Metadata = clonePolicyMap(e.Metadata)
	e.PaymentIntentID = clonePolicyPtr(e.PaymentIntentID)
	return e, reply.err
}
func (g *policyGateway) Create(ctx context.Context, s Snapshot) (SessionEvidence, error) {
	return g.invoke(ctx, "POST", s, "")
}
func (g *policyGateway) Retrieve(ctx context.Context, id string) (SessionEvidence, error) {
	return g.invoke(ctx, "GET", Snapshot{}, id)
}
func policyOpenReply() policyReply {
	return policyReply{run: func(_ context.Context, s Snapshot, _ string) (SessionEvidence, error) { return policyEvidence(s), nil }}
}
