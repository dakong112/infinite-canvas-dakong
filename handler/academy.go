package handler

import (
	"net/http"
	"strconv"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func AcademyCourses(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (any, error) { return service.ListPublishedAcademyCourses(r.URL.Query().Get("category")) })
}

func AcademyCourse(w http.ResponseWriter, r *http.Request, slug string) {
	respond(w, func() (any, error) { return service.GetPublishedAcademyCourse(r.Context(), slug) })
}

func EnrollAcademyCourse(w http.ResponseWriter, r *http.Request, slug string) {
	user, _ := service.UserFromContext(r.Context())
	respond(w, func() (any, error) { return service.EnrollAcademyCourse(user, slug) })
}

func AcademyEnrollments(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	respond(w, func() (any, error) { return service.ListUserAcademyEnrollments(user.ID) })
}

func AdminAcademyCourses(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (any, error) { return service.AdminListAcademyCourses() })
}

func AdminSaveAcademyCourse(w http.ResponseWriter, r *http.Request, id string) {
	var item model.AcademyCourse
	if !decodeEcommerceBody(w, r, &item) {
		return
	}
	if id != "" && academyID(id) == 0 {
		Fail(w, "课程不存在")
		return
	}
	respond(w, func() (any, error) { return service.SaveAcademyCourse(academyID(id), item) })
}

func AdminDeleteAcademyCourse(w http.ResponseWriter, r *http.Request, id string) {
	respond(w, func() (any, error) { return true, service.DeleteAcademyCourse(academyID(id)) })
}

func AdminAcademyEnrollments(w http.ResponseWriter, r *http.Request, id string) {
	respond(w, func() (any, error) { return service.AdminListAcademyEnrollments(academyID(id)) })
}

func AccountApps(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	respond(w, func() (any, error) { return service.AccountAppsForUser(user.ID) })
}

// academyID parses a course id; anything else is 0, which matches no course.
func academyID(value string) uint {
	id, _ := strconv.ParseUint(value, 10, 32)
	return uint(id)
}
