package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

// StorageConfig 返回公开存储配置。
func StorageConfig(w http.ResponseWriter, r *http.Request) {
	config, err := service.PublicStorageConfig()
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, config)
}

// SaveUserStorageProvider 保存用户配置的存储提供商。
func SaveUserStorageProvider(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Provider service.UserStorageProviders `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		Fail(w, "配置内容格式错误")
		return
	}
	config, err := service.SaveCurrentUserStorageProvider(r.Context(), request.Provider)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, config)
}

// MeasureUserStorageProvider 统计用户存储提供商的已用容量。
func MeasureUserStorageProvider(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Provider service.StorageObjectProviderInput `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		Fail(w, "配置内容格式错误")
		return
	}
	result, err := service.MeasureUserStorageProvider(r.Context(), request.Provider)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

// uploadFileMaxBytes caps one upload (lesson videos are the largest, 500MB); per-use limits such as
// the 10MB of image inputs are checked where the file is used.
var uploadFileMaxBytes int64 = 500 << 20

// UploadFile 上传文件到对象存储：表单超过 8MB 的部分由 net/http 落到临时文件，再流式写入存储，内存占用与文件大小无关。
func UploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, uploadFileMaxBytes+1<<20) // + multipart headers
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			Fail(w, "文件不能超过 500MB")
			return
		}
		Fail(w, "请选择要上传的文件")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		Fail(w, "请选择要上传的文件")
		return
	}
	defer file.Close()
	if header.Size > uploadFileMaxBytes {
		Fail(w, "文件不能超过 500MB")
		return
	}
	contentType := header.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		head := make([]byte, 512)
		n, _ := io.ReadFull(file, head)
		contentType = http.DetectContentType(head[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			FailError(w, err)
			return
		}
	}
	var provider *service.StorageObjectProviderInput
	if raw := strings.TrimSpace(r.FormValue("provider")); raw != "" {
		var parsed service.StorageObjectProviderInput
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			Fail(w, "用户对象存储配置格式错误")
			return
		}
		provider = &parsed
	}
	object, err := service.UploadStorageObjectStream(r.Context(), header.Filename, contentType, file, header.Size, provider)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, object)
}

// RegisterDirectFile 登记浏览器已直传至用户 WebDAV 的文件。
func RegisterDirectFile(w http.ResponseWriter, r *http.Request) {
	var request service.DirectStorageObjectInput
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		Fail(w, "文件信息格式错误")
		return
	}
	object, err := service.RegisterDirectStorageObject(r.Context(), request)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, object)
}

// DeleteFile 删除文件。
func DeleteFile(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Provider *service.StorageObjectProviderInput `json:"provider"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&request)
	}
	if err := service.DeleteStorageObject(r.Context(), id, request.Provider); err != nil {
		FailError(w, err)
		return
	}
	OK(w, true)
}

// DeleteDirectFileRecord 删除浏览器已直接删除的 WebDAV 文件索引。
func DeleteDirectFileRecord(w http.ResponseWriter, r *http.Request, id string) {
	if err := service.DeleteDirectStorageObjectRecord(r.Context(), id); err != nil {
		FailError(w, err)
		return
	}
	OK(w, true)
}

// inlineFileContentTypes are the media types safe to render inline; everything else (SVG, HTML, ...) downloads.
var inlineFileContentTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true, "image/avif": true, "image/bmp": true,
	"video/mp4": true, "video/webm": true, "video/quicktime": true,
	"audio/mpeg": true, "audio/mp3": true, "audio/wav": true, "audio/x-wav": true, "audio/ogg": true, "audio/mp4": true, "audio/aac": true, "audio/webm": true, "audio/flac": true,
	"audio/x-m4a": true, "audio/m4a": true, "video/ogg": true, "image/apng": true,
}

func fileMediaType(value string) string {
	mediaType, _, _ := strings.Cut(value, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// setFileSecurityHeaders sets the content type and keeps user-controlled files from running as a page on our origin.
func setFileSecurityHeaders(w http.ResponseWriter, mimeType string) {
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if !inlineFileContentTypes[fileMediaType(mimeType)] {
		w.Header().Set("Content-Disposition", "attachment")
	}
}

// FileContent 获取文件内容：非公开文件需带有效签名（?exp=&sig=）或由所有者登录访问；
// 配置了公开地址的 S3/R2 文件重定向到公开地址（公开文件）或预签名地址（私有文件）。
func FileContent(w http.ResponseWriter, r *http.Request, id string) {
	object, err := service.StorageObjectInfo(id)
	if err != nil {
		FailError(w, err)
		return
	}
	exp, sig := r.URL.Query().Get("exp"), r.URL.Query().Get("sig")
	if !service.FileReadable(r.Context(), object, exp, sig) {
		FailWithStatus(w, http.StatusForbidden, "文件链接已失效或无权访问")
		return
	}
	if link := service.FileRedirectURL(object); link != "" {
		w.Header().Set("Cache-Control", fileCacheControl(object, exp, sig))
		http.Redirect(w, r, link, http.StatusFound)
		return
	}
	download, err := service.DownloadStorageObject(id, r.Header.Get("Range"))
	if err != nil {
		FailError(w, err)
		return
	}
	defer download.Stream.Close()
	setFileSecurityHeaders(w, download.Object.MimeType)
	w.Header().Set("Cache-Control", fileCacheControl(object, exp, sig))
	if download.AcceptRanges {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	if download.ContentRange != "" {
		w.Header().Set("Content-Range", download.ContentRange)
	}
	if download.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(download.ContentLength, 10))
	}
	if download.StatusCode != 0 {
		w.WriteHeader(download.StatusCode)
	}
	_, _ = io.Copy(w, download.Stream)
}

// fileCacheControl: public files cache for good, signed URLs until they expire, and a response read with the
// owner's (or an admin's) credentials is never stored, so it cannot be replayed from a shared or later cache.
func fileCacheControl(object model.StorageObject, exp string, sig string) string {
	if object.Public {
		return "public, max-age=31536000, immutable"
	}
	if expiry, ok := service.SignedFileExpiry(object.ID, exp, sig); ok {
		return "private, max-age=" + strconv.FormatInt(expiry-time.Now().Unix(), 10)
	}
	if !config.Cfg.FileURLSigning {
		return "private, max-age=31536000"
	}
	return "private, no-store"
}

// FileInfo 获取文件元数据；url 为内容地址，所有者登录时返回签名地址。存储位置只给所有者、管理员或公开文件。
func FileInfo(w http.ResponseWriter, r *http.Request, id string) {
	object, err := service.StorageObjectInfo(id)
	if err != nil {
		FailError(w, err)
		return
	}
	if !object.Public && !service.CanSignFile(r.Context(), object) {
		object = model.StorageObject{ID: object.ID, MimeType: object.MimeType, Bytes: object.Bytes, Width: object.Width, Height: object.Height}
	}
	OK(w, struct {
		model.StorageObject
		URL string `json:"url"`
	}{object, "/api/files/" + object.ID + "/content"})
}

// SignFiles 返回文件内容地址（id -> url）；OK 响应只为当前用户可读的文件签名。
func SignFiles(w http.ResponseWriter, r *http.Request) {
	var request struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.IDs) > 200 {
		Fail(w, "文件列表格式错误")
		return
	}
	urls := map[string]string{}
	for _, id := range request.IDs {
		urls[id] = "/api/files/" + id + "/content"
	}
	OK(w, urls)
}

// AdminMeasureStorageProvider 管理员统计存储容量。
func AdminMeasureStorageProvider(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Index    int                    `json:"index"`
		Provider *model.StorageProvider `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		Fail(w, "配置内容格式错误")
		return
	}
	result, err := service.MeasureAdminStorageProvider(request.Index, request.Provider)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

// ProxyImage 代理图片请求（解决跨域和机器人检测问题）。
func ProxyImage(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		Fail(w, "url 参数不能为空")
		return
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		Fail(w, "无效的 url")
		return
	}
	client := service.SafeProxyHTTPClient()
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		FailError(w, err)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	resp, err := client.Do(req)
	if err != nil {
		FailWithStatus(w, http.StatusBadGateway, "代理图片请求失败")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		FailWithStatus(w, http.StatusBadGateway, "代理图片请求失败: "+resp.Status)
		return
	}
	contentType := resp.Header.Get("Content-Type")
	isImage := strings.HasPrefix(contentType, "image/")
	var data []byte
	if isImage {
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			FailWithStatus(w, http.StatusBadGateway, "代理图片请求失败")
			return
		}
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(resp.StatusCode)
	if isImage {
		_, _ = w.Write(data)
		return
	}
	_, _ = io.Copy(w, resp.Body)
}
