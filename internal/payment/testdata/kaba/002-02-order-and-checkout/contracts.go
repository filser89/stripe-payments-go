package payment

import (
	"context"
	"log/slog"
	"time"
)

type CreateInput struct {
	Description string
	Amount      int64
	RequestKey  string
}
type Order struct {
	ID                 string
	Description        string
	Amount             int64
	Currency           string
	Status             string
	CurrentOperationID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
type Snapshot struct {
	OrderID         string
	OperationID     string
	Description     string
	Amount          int64
	Currency        string
	StripeKey       string
	SuccessURL      string
	CancelURL       string
	SDKVersion      string
	APIVersion      string
	FirstDispatchAt *time.Time
	LastDispatchAt  *time.Time
	ExpiresAt       int64
}
type Operation struct {
	ID                    string
	OrderID               string
	State                 string
	StripeKey             string
	Snapshot              Snapshot
	SessionID             *string
	PaymentIntentID       *string
	CheckoutURL           *string
	ExpiresAt             *time.Time
	FirstDispatchAt       *time.Time
	LastDispatchAt        *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	PriorAmbiguity        bool
	Version               int64
	OwnerToken            string
	EvidenceSource        string
	FailureCode           *string
	InvestigationRequired bool
}
type RequestBinding struct {
	View        View
	Key         string
	Method      string
	Target      string
	OrderID     string
	OperationID string
	Description string
	Amount      int64
	Currency    string
	CreatedAt   time.Time
}
type HistoryEntry struct {
	Sequence        int64
	OrderID         string
	OperationID     string
	Kind            string
	State           string
	ObservedAt      time.Time
	SessionID       *string
	PaymentIntentID *string
	EventID         *string
	RequestID       *string
	EventAt         *time.Time
	FromState       *string
	ToState         *string
	FailureCode     *string
}
type View struct {
	Order                 Order
	Operation             Operation
	CanResume             bool
	CanRetrySameOperation bool
	CanStartNewAttempt    bool
	NeedsInvestigation    bool
}
type Outcome struct {
	Order                 Order
	Operation             Operation
	CanResume             bool
	CanRetrySameOperation bool
	CanStartNewAttempt    bool
	NeedsInvestigation    bool
	NewlyAccepted         bool
	Established           bool
	Pending               bool
	ConfirmedRejected     bool
}
type HistoryPage struct {
	OrderID   string
	Entries   []HistoryEntry
	NextAfter *int64
}
type SessionEvidence struct {
	SessionID         string
	ClientReferenceID string
	Metadata          map[string]string
	AmountTotal       int64
	Currency          string
	Mode              string
	Livemode          bool
	Status            string
	PaymentStatus     string
	PaymentIntentID   *string
	URL               string
	ExpiresAt         int64
	RequestID         string
	ObservedAt        time.Time
	ErrorClass        string
	ErrorCode         string
	RetryAfter        time.Duration
	StripeShouldRetry *bool
}
type Error struct {
	Code        string
	OrderID     string
	OperationID string
	Cause       error
}

func (e *Error) Error() string { return "" }
func (e *Error) Unwrap() error { return nil }

type Options struct {
	Currency       string
	MinAmount      int64
	MaxAmount      int64
	Origin         string
	SDKVersion     string
	APIVersion     string
	RequestTimeout time.Duration
	CallTimeout    time.Duration
	RetryBudget    time.Duration
	MaxAttempts    int
	Logger         *slog.Logger
	Now            func() time.Time
	Wait           func(context.Context, time.Duration) error
}
type AcceptedIntent struct {
	Order     Order
	Operation Operation
	Binding   RequestBinding
	History   HistoryEntry
}
type ContinuationIntent struct {
	OrderID                    string
	ExpectedCurrentOperationID string
	ExpectedVersion            int64
	Operation                  Operation
	Binding                    RequestBinding
	History                    HistoryEntry
}
type Dispatch struct {
	OrderID                    string
	OperationID                string
	ExpectedVersion            int64
	ExpectedCurrentOperationID string
	OwnerToken                 string
	Snapshot                   Snapshot
	FirstDispatchAt            time.Time
	LastDispatchAt             time.Time
}
type Observation struct {
	OrderID                    string
	OperationID                string
	ExpectedVersion            int64
	ExpectedCurrentOperationID string
	OwnerToken                 string
	State                      string
	Evidence                   SessionEvidence
	PriorAmbiguity             bool
	History                    HistoryEntry
}
type Repository interface {
	LoadBinding(context.Context, string) (RequestBinding, error)
	AcceptInitial(context.Context, AcceptedIntent) (View, error)
	LoadOrder(context.Context, string) (View, error)
	BindContinuation(context.Context, ContinuationIntent) (View, error)
	PrepareDispatch(context.Context, Dispatch) (Operation, error)
	ApplyObservation(context.Context, Observation) (View, error)
	ReadHistory(context.Context, string, int64, int) (HistoryPage, error)
}
type Gateway interface {
	Create(context.Context, Snapshot) (SessionEvidence, error)
	Retrieve(context.Context, string) (SessionEvidence, error)
}
type Operations interface {
	Create(context.Context, CreateInput) (Outcome, error)
	Continue(context.Context, string, string) (Outcome, error)
	Get(context.Context, string) (View, error)
	History(context.Context, string, int64, int) (HistoryPage, error)
}
type Service struct{}

func New(Repository, Gateway, Options) *Service                                           { return nil }
func (*Service) Create(context.Context, CreateInput) (out Outcome, err error)             { return }
func (*Service) Continue(context.Context, string, string) (out Outcome, err error)        { return }
func (*Service) Get(context.Context, string) (out View, err error)                        { return }
func (*Service) History(context.Context, string, int64, int) (out HistoryPage, err error) { return }
