package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

// requestWriter carries the gin request so JSON responses can sign file URLs for its user.
type requestWriter struct {
	gin.ResponseWriter
	c *gin.Context
}

// SignFileURLs is router middleware: OK responses then sign the stored-file URLs they contain
// (see service.SignFileURLsInJSON), whichever handler builds them.
func SignFileURLs(c *gin.Context) {
	c.Writer = requestWriter{c.Writer, c}
	c.Next()
}

type response struct {
	Code int    `json:"code"`
	Data any    `json:"data"`
	Msg  string `json:"msg"`
}

func OK(w http.ResponseWriter, data any) {
	writeJSON(w, response{Code: 0, Data: data, Msg: "ok"})
}

func Fail(w http.ResponseWriter, msg string) {
	writeJSON(w, response{Code: 1, Data: nil, Msg: msg})
}

func FailWithStatus(w http.ResponseWriter, status int, msg string) {
	writeJSONWithStatus(w, status, response{Code: 1, Data: nil, Msg: msg})
}

func writeJSONWithStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func FailError(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	if safe, ok := err.(interface{ SafeMessage() string }); ok {
		Fail(w, safe.SafeMessage())
		return
	}
	Fail(w, "操作失败")
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(value)
	if rw, ok := w.(requestWriter); ok {
		body = service.SignFileURLsInJSON(rw.c.Request.Context(), body)
	}
	_, _ = w.Write(append(body, '\n'))
}

func parseQuery(r *http.Request) model.Query {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	return model.Query{
		Keyword:  q.Get("keyword"),
		Date:     q.Get("date"),
		Tags:     q["tag"],
		Category: q.Get("category"),
		Type:     q.Get("type"),
		Page:     page,
		PageSize: pageSize,
	}
}
