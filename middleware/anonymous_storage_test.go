package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/handler"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

// An anonymous uploader gets their file's details and fresh signed URLs through the storage session cookie.
func TestAnonymousOwnerRefreshesFileURLs(t *testing.T) {
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1,
		LocalStorageDir: t.TempDir(), FileURLSigning: true}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(handler.SignFileURLs)
	engine.POST("/api/anonymous/files/session", AnonymousStorage, func(c *gin.Context) {
		user, _ := service.UserFromContext(c.Request.Context())
		c.String(http.StatusOK, user.ID)
	})
	engine.GET("/api/files/:id", OptionalFileAuth, func(c *gin.Context) { handler.FileInfo(c.Writer, c.Request, c.Param("id")) })
	engine.POST("/api/files/sign", OptionalFileAuth, gin.WrapF(handler.SignFiles))

	session := httptest.NewRecorder()
	engine.ServeHTTP(session, httptest.NewRequest(http.MethodPost, "/api/anonymous/files/session", nil))
	cookies := session.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/api" {
		t.Fatalf("session cookie: %v", cookies)
	}
	uploaded, err := repository.SaveStorageObject(model.StorageObject{ID: "anon-file", ObjectKey: "k", MimeType: "image/png", CreatedBy: session.Body.String()})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method string, path string, body string, withCookie bool) string {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if withCookie {
			request.AddCookie(cookies[0])
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder.Body.String()
	}
	var info struct {
		Data struct {
			ObjectKey string `json:"objectKey"`
			URL       string `json:"url"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(send(http.MethodGet, "/api/files/"+uploaded.ID, "", true)), &info)
	if info.Data.ObjectKey == "" || !strings.Contains(info.Data.URL, "sig=") {
		t.Errorf("owner info: %+v", info.Data)
	}
	_ = json.Unmarshal([]byte(send(http.MethodGet, "/api/files/"+uploaded.ID, "", false)), &info)
	if info.Data.ObjectKey != "" || strings.Contains(info.Data.URL, "sig=") {
		t.Errorf("info without the cookie: %+v", info.Data)
	}
	var signed struct {
		Data map[string]string `json:"data"`
	}
	_ = json.Unmarshal([]byte(send(http.MethodPost, "/api/files/sign", `{"ids":["`+uploaded.ID+`"]}`, true)), &signed)
	if !strings.Contains(signed.Data[uploaded.ID], "sig=") {
		t.Errorf("sign with the cookie: %v", signed.Data)
	}
}
