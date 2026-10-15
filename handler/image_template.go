package handler

import (
	"net/http"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func ImageTemplates(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (any, error) { return service.ListPublishedImageTemplates(r.URL.Query().Get("category")) })
}

func ImageTemplate(w http.ResponseWriter, r *http.Request, slug string) {
	respond(w, func() (any, error) { return service.GetPublishedImageTemplate(slug) })
}

func GenerateWithImageTemplate(w http.ResponseWriter, r *http.Request, slug string) {
	var input service.TemplateGenerateInput
	if !decodeEcommerceBody(w, r, &input) {
		return
	}
	respondEcommerce(w, r, func(user model.AuthUser) (any, error) {
		batch, jobs, err := service.GenerateWithImageTemplate(r.Context(), user, slug, input)
		if err != nil {
			return nil, err
		}
		return map[string]any{"batchId": batch.ID, "jobs": jobs}, nil
	})
}

func AdminImageTemplates(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (any, error) { return service.AdminListImageTemplates() })
}

func AdminSaveImageTemplate(w http.ResponseWriter, r *http.Request, id string) {
	var item model.ImageTemplate
	if !decodeEcommerceBody(w, r, &item) {
		return
	}
	respond(w, func() (any, error) { return service.SaveImageTemplate(id, item) })
}

func AdminTemplateFees(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (any, error) { return service.AdminListTemplateFees(r.URL.Query().Get("status")) })
}

func AdminDeleteImageTemplate(w http.ResponseWriter, r *http.Request, id string) {
	respond(w, func() (any, error) { return true, service.DeleteImageTemplate(id) })
}

func respond(w http.ResponseWriter, action func() (any, error)) {
	result, err := action()
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}
