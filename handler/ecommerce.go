package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

// respondEcommerce runs an ecommerce action for the signed-in user and writes the standard envelope.
func respondEcommerce(w http.ResponseWriter, r *http.Request, action func(user model.AuthUser) (any, error)) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	result, err := action(user)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

func decodeEcommerceBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(target); err != nil {
		Fail(w, "请求参数无效")
		return false
	}
	return true
}

func ListEcommerceSets(w http.ResponseWriter, r *http.Request) {
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.ListEcommerceSets(user.ID, r.URL.Query().Get("preset"))
	})
}

func CreateEcommerceSet(w http.ResponseWriter, r *http.Request) {
	var form service.EcommerceSetForm
	if !decodeEcommerceBody(w, r, &form) {
		return
	}
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.CreateEcommerceSet(r.Context(), user, form)
	})
}

func GetEcommerceSet(w http.ResponseWriter, r *http.Request, id string) {
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.GetEcommerceSet(r.Context(), user, id)
	})
}

func UpdateEcommerceSet(w http.ResponseWriter, r *http.Request, id string) {
	var form service.EcommerceSetForm
	if !decodeEcommerceBody(w, r, &form) {
		return
	}
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.UpdateEcommerceSet(r.Context(), user, id, form)
	})
}

func AnalyzeEcommerceSet(w http.ResponseWriter, r *http.Request, id string) {
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.AnalyzeEcommerceSet(r.Context(), user, id)
	})
}

func EcommerceSellingPoints(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Kind string `json:"kind"`
		Mode string `json:"mode"`
	}
	if !decodeEcommerceBody(w, r, &request) {
		return
	}
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.RequestEcommerceSellingPoints(r.Context(), user, id, request.Kind, request.Mode)
	})
}

func PlanEcommerceSet(w http.ResponseWriter, r *http.Request, id string) {
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.PlanEcommerceSet(r.Context(), user, id)
	})
}

func GenerateEcommerceSet(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Stage   string   `json:"stage"`
		SlotIDs []string `json:"slotIds"`
	}
	if !decodeEcommerceBody(w, r, &request) {
		return
	}
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		return service.GenerateEcommerceSet(r.Context(), user, id, request.Stage, request.SlotIDs)
	})
}

func RunEcommerceTool(w http.ResponseWriter, r *http.Request, tool string) {
	var input service.EcommerceToolInput
	if !decodeEcommerceBody(w, r, &input) {
		return
	}
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		batch, jobs, err := service.RunEcommerceTool(r.Context(), user, tool, input)
		if err != nil {
			return nil, err
		}
		return map[string]any{"batchId": batch.ID, "jobs": jobs}, nil
	})
}
