package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tigerowo/infinite-canvas/service"
)

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
	_ = json.NewDecoder(r.Body).Decode(&request)
	if err := service.SubscribeWithBalance(user.ID, request.PlanID); err != nil {
		FailError(w, err)
		return
	}
	AccountSummary(w, r)
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
	_ = json.NewDecoder(r.Body).Decode(&request)
	if err := service.RedeemTopupCode(user.ID, request.Key); err != nil {
		FailError(w, err)
		return
	}
	AccountSummary(w, r)
}

func AccountEpay(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	var request struct {
		Amount        int64  `json:"amount"`
		PaymentMethod string `json:"paymentMethod"`
	}
	_ = json.NewDecoder(r.Body).Decode(&request)
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
