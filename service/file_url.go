package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// fileURLPattern matches a stored-file content URL in plain or JSON text; in JSON its query's & may read &.
var fileURLPattern = regexp.MustCompile(`/api/files/([A-Za-z0-9-]+)/content(\?(?:[A-Za-z0-9=_%.~-]|&|\\u0026)*)?`)

// fileURLTTL is how long a signed file URL stays valid.
const fileURLTTL = 7 * 24 * time.Hour

// fileURLSignature is base64url(HMAC-SHA256(HMAC(JWT_SECRET, "file-url-v1"), id + "." + exp)).
func fileURLSignature(id string, exp int64) string {
	key := hmac.New(sha256.New, []byte(config.Cfg.JWTSecret))
	key.Write([]byte("file-url-v1"))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write([]byte(id + "." + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// canSignFile reports whether the user in ctx owns object or is an admin.
func canSignFile(ctx context.Context, object model.StorageObject) bool {
	user, ok := UserFromContext(ctx)
	return ok && user.ID != "" && (user.ID == object.CreatedBy || user.Role == model.UserRoleAdmin)
}

// FileReadable reports whether a content request may read object: a public file, signing turned off,
// an unexpired valid signature, or its owner (or an admin) signed in.
func FileReadable(ctx context.Context, object model.StorageObject, exp string, sig string) bool {
	if object.Public || !config.Cfg.FileURLSigning {
		return true
	}
	expiry, err := strconv.ParseInt(exp, 10, 64)
	if err == nil && time.Now().Unix() <= expiry && hmac.Equal([]byte(sig), []byte(fileURLSignature(object.ID, expiry))) {
		return true
	}
	return canSignFile(ctx, object)
}

// fileURL builds the content URL of id with query minus any old signature; exp > 0 signs it.
func fileURL(id string, query url.Values, exp int64) string {
	query.Del("exp")
	query.Del("sig")
	if exp > 0 {
		query.Set("exp", strconv.FormatInt(exp, 10))
		query.Set("sig", fileURLSignature(id, exp))
	}
	if len(query) == 0 {
		return "/api/files/" + id + "/content"
	}
	return "/api/files/" + id + "/content?" + query.Encode()
}

func fileURLQuery(raw string) url.Values {
	query, _ := url.ParseQuery(strings.ReplaceAll(strings.TrimPrefix(raw, "?"), `&`, "&"))
	return query
}

// SignFileURLsInJSON rewrites the stored-file URLs in a JSON response body: public files drop any signature,
// files the user in ctx may read get a fresh one, the rest stay as they are. Stored values are never touched.
func SignFileURLsInJSON(ctx context.Context, body []byte) []byte {
	matches := fileURLPattern.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return body
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, string(match[1]))
	}
	objects, err := repository.GetStorageObjects(slices.Compact(slices.Sorted(slices.Values(ids))))
	if err != nil {
		log.Printf("sign file urls: %v", err)
		return body
	}
	// Whole hours keep a URL stable across reloads, so browsers can cache it.
	exp := time.Now().Add(fileURLTTL).Truncate(time.Hour).Unix()
	return fileURLPattern.ReplaceAllFunc(body, func(match []byte) []byte {
		parts := fileURLPattern.FindSubmatch(match)
		object, ok := objects[string(parts[1])]
		switch {
		case ok && object.Public:
			return []byte(fileURL(object.ID, fileURLQuery(string(parts[2])), 0))
		case ok && config.Cfg.FileURLSigning && canSignFile(ctx, object):
			return []byte(fileURL(object.ID, fileURLQuery(string(parts[2])), exp))
		}
		return match
	})
}

// unsignedFileURL drops exp/sig from a stored-file path so templates and settings save the plain URL.
func unsignedFileURL(value string) string {
	value = strings.TrimSpace(value)
	if parts := fileURLPattern.FindStringSubmatch(value); parts != nil && parts[0] == value {
		return fileURL(parts[1], fileURLQuery(parts[2]), 0)
	}
	return value
}

// markFilesPublic flags the stored files behind links as public.
func markFilesPublic(links []string) error {
	ids := []string{}
	for _, link := range links {
		if parts := fileURLPattern.FindStringSubmatch(link); parts != nil {
			ids = append(ids, parts[1])
		}
	}
	return repository.MarkStorageObjectsPublic(ids)
}

// settingsFileLinks lists the image URLs of the tool examples (public.imageExamples) and template samples.
func settingsFileLinks(settings model.Settings) []string {
	samples := settings.Private.TemplateSamples
	links := slices.Concat(samples.Product, samples.Portrait, samples.Model)
	for _, entry := range settings.Public.ImageExamples {
		links = append(links, entry.Cover)
		for _, item := range entry.Items {
			links = append(links, item.URL, item.Before)
		}
	}
	return links
}

// MarkReferencedFilesPublic flags the files templates and settings already point at; it is idempotent and runs at boot.
func MarkReferencedFilesPublic() error {
	settings, err := repository.GetSettings()
	if err != nil {
		return err
	}
	if err := markFilesPublic(settingsFileLinks(settings)); err != nil {
		return err
	}
	templates, err := repository.ListImageTemplates("", "")
	if err != nil {
		return err
	}
	for _, item := range templates {
		if err := repository.MarkStorageObjectsPublic(templateFileIDs(item)); err != nil {
			return err
		}
	}
	return nil
}
