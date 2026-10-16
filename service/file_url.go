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

// fileURLPattern matches a stored-file content URL in plain or JSON text; in JSON its query's & reads \u0026.
var fileURLPattern = regexp.MustCompile(`/api/files/([A-Za-z0-9-]+)/content(\?(?:[A-Za-z0-9=_%.~+-]|&|\\u0026)*)?`)

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

// CanSignFile reports whether the user in ctx owns object (or shares its team space) or is a local admin.
func CanSignFile(ctx context.Context, object model.StorageObject) bool {
	user, ok := UserFromContext(ctx)
	return ok && user.ID != "" && (fileInUserSpace(user, object) || user.IsLocalAdmin)
}

// FileReadable reports whether a content request may read object: a public file, signing turned off,
// an unexpired valid signature, or its owner (or an admin) signed in.
func FileReadable(ctx context.Context, object model.StorageObject, exp string, sig string) bool {
	if object.Public || !config.Cfg.FileURLSigning {
		return true
	}
	if _, ok := SignedFileExpiry(object.ID, exp, sig); ok {
		return true
	}
	return CanSignFile(ctx, object)
}

// SignedFileExpiry returns the expiry of exp/sig when they are an unexpired valid signature of id.
func SignedFileExpiry(id string, exp string, sig string) (int64, bool) {
	expiry, err := strconv.ParseInt(exp, 10, 64)
	return expiry, err == nil && time.Now().Unix() <= expiry && hmac.Equal([]byte(sig), []byte(fileURLSignature(id, expiry)))
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

// fileURLQuery parses a matched query, plain or JSON-escaped (\u0026 between params).
func fileURLQuery(raw string) url.Values {
	query, _ := url.ParseQuery(strings.ReplaceAll(strings.TrimPrefix(raw, "?"), `\u0026`, "&"))
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
	// Written back with \u0026 for &, the way json.Marshal escapes it.
	jsonURL := func(link string) []byte { return []byte(strings.ReplaceAll(link, "&", `\u0026`)) }
	return fileURLPattern.ReplaceAllFunc(body, func(match []byte) []byte {
		parts := fileURLPattern.FindSubmatch(match)
		object, ok := objects[string(parts[1])]
		switch {
		case ok && object.Public:
			return jsonURL(fileURL(object.ID, fileURLQuery(string(parts[2])), 0))
		case ok && config.Cfg.FileURLSigning && CanSignFile(ctx, object):
			return jsonURL(fileURL(object.ID, fileURLQuery(string(parts[2])), exp))
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
	return repository.SetStorageObjectsPublic(fileIDsOf(links), true)
}

func fileIDsOf(links []string) []string {
	ids := []string{}
	for _, link := range links {
		if parts := fileURLPattern.FindStringSubmatch(link); parts != nil {
			ids = append(ids, parts[1])
		}
	}
	return ids
}

// settingsFileLinks lists the image URLs of the tool examples (public.imageExamples) and template samples,
// and the background music library.
func settingsFileLinks(settings model.Settings) []string {
	samples := settings.Private.TemplateSamples
	links := slices.Concat(samples.Product, samples.Portrait, samples.Model)
	for _, track := range settings.Private.BgmTracks {
		links = append(links, track.URL)
	}
	for _, entry := range settings.Public.ImageExamples {
		links = append(links, entry.Cover)
		for _, item := range entry.Items {
			links = append(links, item.URL, item.Before)
		}
	}
	return links
}

// referencedFileIDs lists the stored files templates and settings point at.
func referencedFileIDs() ([]string, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return nil, err
	}
	templates, err := repository.ListImageTemplates("", "", "")
	if err != nil {
		return nil, err
	}
	ids := fileIDsOf(settingsFileLinks(settings))
	for _, item := range templates {
		ids = append(ids, templateFileIDs(item)...)
	}
	return ids, nil
}

// MarkReferencedFilesPublic flags the files templates and settings already point at; it is idempotent and runs at boot.
func MarkReferencedFilesPublic() error {
	ids, err := referencedFileIDs()
	if err != nil {
		return err
	}
	return repository.SetStorageObjectsPublic(ids, true)
}
