package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/tigerowo/infinite-canvas/service"
)

// decodeAccountBody reads a small JSON body; on failure it answers Fail and returns false.
func decodeAccountBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(out); err != nil {
		Fail(w, "请求参数无效")
		return false
	}
	return true
}

// respondSummaryAfterWrite answers after a successful write; a failed refresh must not look like a failed write.
func respondSummaryAfterWrite(w http.ResponseWriter, userID string) {
	summary, err := service.AccountSummaryForUser(userID)
	if err != nil {
		log.Printf("account summary after write failed: %v", err)
		OK(w, nil)
		return
	}
	OK(w, summary)
}

func AccountSummary(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	summary, err := service.AccountSummaryForUser(user.ID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, summary)
}

func AccountPlans(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	plans, err := service.AccountPlansForUser(user.ID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, plans)
}

func AccountSubscribe(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	var request struct {
		PlanID int `json:"planId"`
	}
	if !decodeAccountBody(w, r, &request) {
		return
	}
	if err := service.SubscribeWithBalance(user.ID, request.PlanID); err != nil {
		FailError(w, err)
		return
	}
	respondSummaryAfterWrite(w, user.ID)
}

func AccountTopupInfo(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	info, err := service.AccountTopupInfoForUser(user.ID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, info)
}

func AccountRedeem(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	var request struct {
		Key string `json:"key"`
	}
	if !decodeAccountBody(w, r, &request) {
		return
	}
	if err := service.RedeemTopupCode(user.ID, request.Key); err != nil {
		FailError(w, err)
		return
	}
	respondSummaryAfterWrite(w, user.ID)
}

func AccountEpay(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	var request struct {
		Amount        int64  `json:"amount"`
		PaymentMethod string `json:"paymentMethod"`
	}
	if !decodeAccountBody(w, r, &request) {
		return
	}
	order, err := service.CreateEpayOrder(user.ID, request.Amount, request.PaymentMethod)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, order)
}

func AccountPricing(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	pricing, err := service.AccountPricingForUser(user.ID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, pricing)
}

func AccountModels(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	catalog, err := service.ModelCatalogForUser(user.ID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, catalog)
}

func AccountNewapiLogin(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	var request struct {
		To string `json:"to"`
	}
	if !decodeAccountBody(w, r, &request) {
		return
	}
	loginURL, err := service.CreateNewapiLoginTicket(user.ID, request.To)
	if err != nil {
		FailError(w, err)
		return
	}
	// The response carries a one-time ticket.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	OK(w, map[string]string{"loginUrl": loginURL})
}

func Spaces(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	spaces, err := service.ListSpaces(user.ID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, spaces)
}

func SwitchSpace(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	var request struct {
		TeamID int `json:"teamId"`
	}
	if !decodeAccountBody(w, r, &request) {
		return
	}
	spaces, err := service.SwitchSpace(user.ID, request.TeamID)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, spaces)
}
