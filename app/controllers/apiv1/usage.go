package apiv1

import (
	"time"

	"github.com/AlexGalhardo/vukem/web"
)

// UsageView is today's processing budget for the caller.
type UsageView struct {
	Unlimited        bool      `json:"unlimited"          doc:"True when no daily budget applies to this account."`
	BudgetSeconds    int64     `json:"budget_seconds"     doc:"Today's budget in seconds of processing time. 0 when unlimited."`
	SpentSeconds     int64     `json:"spent_seconds"      doc:"Processing time spent today, in seconds."`
	RemainingSeconds int64     `json:"remaining_seconds"  doc:"What is left today, in seconds. 0 when unlimited or spent: read unlimited to tell them apart."`
	ResetsAt         time.Time `json:"resets_at"          doc:"When the next day's budget starts (midnight UTC)."`
	StoredBytes      int64     `json:"stored_bytes"       doc:"Bytes of yours that are stored and have not expired: uploads and results."`
	StoredLimitBytes int64     `json:"stored_limit_bytes" doc:"The most bytes an account may store at once. 0 when there is no cap."`
}

// Usage reports what the caller has spent today: GET /api/v1/usage.
func (h *Conversions) Usage(c *web.Ctx, _ struct{}) (UsageView, error) {
	userID, err := callerID(c)
	if err != nil {
		return UsageView{}, err
	}

	if h.quota == nil {
		return UsageView{Unlimited: true, ResetsAt: time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)}, nil
	}

	a, err := h.quota.Check(c.Context(), userID)
	if err != nil {
		return UsageView{}, err
	}

	stored, err := h.files.StoredBytes(c.Context(), userID)
	if err != nil {
		return UsageView{}, err
	}

	return UsageView{
		Unlimited:        a.Budget == 0 || a.Exempt,
		BudgetSeconds:    int64(a.Budget / time.Second),
		SpentSeconds:     int64(a.Spent / time.Second),
		RemainingSeconds: int64(a.Remaining() / time.Second),
		ResetsAt:         a.ResetsAt,
		StoredBytes:      stored,
		StoredLimitBytes: h.files.MaxStoredBytes(),
	}, nil
}
