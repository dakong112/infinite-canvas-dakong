package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	academyMaxLessons    = 100
	academyMaxBodyRunes  = 20000
	academyMaxCapacity   = 100000
	academyMaxFeeCredits = 100000
)

var errAcademyNotFound = safeMessageError{message: "课程不存在"}

// ListPublishedAcademyCourses is the public course list: body and lessons stay on the detail page.
func ListPublishedAcademyCourses(category string) ([]model.AcademyCourse, error) {
	items, err := repository.ListAcademyCourses(model.AcademyStatusPublished, strings.TrimSpace(category))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range items {
		items[i].Body, items[i].Lessons, items[i].State = "", nil, academyState(items[i], now)
	}
	return items, nil
}

// GetPublishedAcademyCourse returns a published course; lesson videos only reach enrolled users or free courses,
// and uploaded lesson files (private, signed links) only enrolled users, free courses included.
func GetPublishedAcademyCourse(ctx context.Context, slug string) (model.AcademyCourse, error) {
	item, err := publishedAcademyCourse(slug)
	if err != nil {
		return item, err
	}
	if user, ok := UserFromContext(ctx); ok && user.ID != "" {
		if _, item.Enrolled, err = repository.GetAcademyEnrollment(item.ID, user.ID); err != nil {
			return item, err
		}
	}
	item.State = academyState(item, time.Now())
	item.Locked = !item.Enrolled && !academyFree(item)
	for i := range item.Lessons {
		link := item.Lessons[i].VideoURL
		if parts := fileURLPattern.FindStringSubmatch(link); item.Locked || parts != nil && parts[0] == link && !item.Enrolled {
			item.Lessons[i].VideoURL = ""
		} else {
			item.Lessons[i].VideoURL = signedFileLink(link)
		}
	}
	return item, nil
}

func publishedAcademyCourse(slug string) (model.AcademyCourse, error) {
	item, ok, err := repository.GetAcademyCourseBySlug(strings.TrimSpace(slug))
	if err != nil {
		return model.AcademyCourse{}, err
	}
	if !ok || item.Status != model.AcademyStatusPublished {
		return model.AcademyCourse{}, errAcademyNotFound
	}
	return item, nil
}

// academyFree: anyone may watch a course without fee or member restriction.
func academyFree(item model.AcademyCourse) bool {
	return item.FeeCredits <= 0 && !item.MemberOnly
}

// academyState derives 报名中 / 已满 / 已结束: ended once the course or its enrollment window is over.
func academyState(item model.AcademyCourse, now time.Time) string {
	if item.EndsAt != nil && now.After(*item.EndsAt) || item.EnrollEndsAt != nil && now.After(*item.EnrollEndsAt) {
		return model.AcademyStateEnded
	}
	if item.Capacity > 0 && item.EnrolledCount >= item.Capacity {
		return model.AcademyStateFull
	}
	return model.AcademyStateOpen
}

// signedFileLink signs a stored-file link so an enrolled user (not its owner) can play it; other links pass through.
func signedFileLink(link string) string {
	parts := fileURLPattern.FindStringSubmatch(link)
	if !config.Cfg.FileURLSigning || parts == nil || parts[0] != link {
		return link
	}
	return fileURL(parts[1], fileURLQuery(parts[2]), time.Now().Add(fileURLTTL).Truncate(time.Hour).Unix())
}

// EnrollAcademyCourse charges first and records after (R-TPL-PAY-3): live membership check, atomic seat
// reservation, new-api charge with idempotency key ac:<courseID>:<userID>, then the enrollment row. Any failure
// after the reservation gives the seat back; a retry after a lost row is never charged twice.
func EnrollAcademyCourse(user model.AuthUser, slug string) (model.AcademyEnrollment, error) {
	course, err := publishedAcademyCourse(slug)
	if err != nil {
		return model.AcademyEnrollment{}, err
	}
	if existing, ok, err := repository.GetAcademyEnrollment(course.ID, user.ID); err != nil || ok {
		return existing, err
	}
	if academyState(course, time.Now()) == model.AcademyStateEnded {
		return model.AcademyEnrollment{}, safeMessageError{message: "报名已结束"}
	}
	member := false
	if course.MemberOnly || course.FeeCredits > 0 && course.MemberFree {
		if member, err = academyMember(user.ID); err != nil {
			return model.AcademyEnrollment{}, err
		}
	}
	if course.MemberOnly && !member {
		return model.AcademyEnrollment{}, safeMessageError{message: "该课程仅限会员报名"}
	}
	// ponytail: without new-api SSO there is no wallet to charge, so paid courses are free (as template fees are).
	var linked model.User
	var quota int64
	if course.FeeCredits > 0 && !(course.MemberFree && member) && NewapiSSOEnabled() {
		if linked, err = newapiLinkedUser(user.ID); err != nil {
			return model.AcademyEnrollment{}, err
		}
		if quota, err = academyFeeQuota(course.FeeCredits); err != nil {
			return model.AcademyEnrollment{}, err
		}
	}
	if ok, err := repository.ReserveAcademySeat(course.ID); err != nil {
		return model.AcademyEnrollment{}, err
	} else if !ok {
		return model.AcademyEnrollment{}, safeMessageError{message: "名额已满"}
	}
	// ponytail: a crash between the reservation and the row leaks one seat; admins can raise the capacity.
	enrollment := model.AcademyEnrollment{CourseID: course.ID, UserID: user.ID, FeeQuota: quota, FeeStatus: model.AcademyFeeFree, CreatedAt: time.Now().UTC()}
	if quota > 0 {
		status, err := newapiConsume(linked, fmt.Sprintf("ac:%d:%s", course.ID, user.ID), quota, course.Title)
		if err != nil {
			releaseAcademySeat(course.ID)
			log.Printf("academy charge failed course=%d user=%s status=%d err=%v", course.ID, user.ID, status, err)
			switch status {
			case http.StatusPaymentRequired:
				return model.AcademyEnrollment{}, safeMessageError{message: "积分不足，请充值后再报名"}
			case http.StatusBadRequest:
				return model.AcademyEnrollment{}, safeMessageError{message: "课程费用超出单次收费上限，请联系管理员"}
			case http.StatusUnauthorized, http.StatusForbidden:
				return model.AcademyEnrollment{}, safeMessageError{message: newapiReauthMessage}
			}
			return model.AcademyEnrollment{}, safeMessageError{message: "扣费失败，请稍后重试"}
		}
		enrollment.FeeStatus = model.AcademyFeeCharged
	}
	created, err := repository.CreateAcademyEnrollment(&enrollment)
	if err != nil || !created {
		releaseAcademySeat(course.ID)
	}
	if err != nil || created {
		return enrollment, err
	}
	// A concurrent request of the same user won; its row is the enrollment (the charge was replayed).
	existing, _, err := repository.GetAcademyEnrollment(course.ID, user.ID)
	return existing, err
}

func releaseAcademySeat(courseID uint) {
	if err := repository.ReleaseAcademySeat(courseID); err != nil {
		log.Printf("release academy seat failed course=%d err=%v", courseID, err)
	}
}

// academyMember checks the membership live in new-api (no summary cache).
func academyMember(userID string) (bool, error) {
	if !NewapiSSOEnabled() {
		return false, nil
	}
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return false, err
	}
	member, err := activeMembership(user, 1, 1) // only Active is read
	return member.Active, err
}

// academyFeeQuota converts the course fee to new-api quota; new-api's per-client consume_max_usd is the cap.
func academyFeeQuota(credits float64) (int64, error) {
	billing, err := publicBillingSetting()
	if err != nil {
		return 0, err
	}
	perUnit, err := newapiQuotaPerUnit()
	if err != nil {
		return 0, err
	}
	return feeQuota(credits, billing.CreditsPerUnit, perUnit, math.Inf(1))
}

func ListUserAcademyEnrollments(userID string) ([]model.AcademyEnrollment, error) {
	return repository.ListUserAcademyEnrollments(userID)
}

func AdminListAcademyCourses() ([]model.AcademyCourse, error) {
	return repository.ListAcademyCourses("", "")
}

func AdminListAcademyEnrollments(id uint) ([]model.AcademyEnrollmentRow, error) {
	return repository.ListAcademyCourseEnrollments(id)
}

// SaveAcademyCourse creates (id 0) or replaces a course; the seat counter is never taken from the input.
func SaveAcademyCourse(id uint, item model.AcademyCourse) (model.AcademyCourse, error) {
	if err := normalizeAcademyCourse(&item); err != nil {
		return model.AcademyCourse{}, err
	}
	if other, ok, err := repository.GetAcademyCourseBySlug(item.Slug); err != nil {
		return model.AcademyCourse{}, err
	} else if ok && other.ID != id {
		return model.AcademyCourse{}, safeMessageError{message: "标识已被其他课程使用"}
	}
	item.UpdatedAt = time.Now().UTC()
	if id == 0 {
		item.ID, item.EnrolledCount, item.CreatedAt = 0, 0, item.UpdatedAt
		if err := repository.CreateAcademyCourse(&item); err != nil {
			return model.AcademyCourse{}, err
		}
	} else {
		existing, ok, err := repository.GetAcademyCourse(id)
		if err != nil {
			return model.AcademyCourse{}, err
		}
		if !ok {
			return model.AcademyCourse{}, errAcademyNotFound
		}
		item.ID, item.EnrolledCount, item.CreatedAt = id, existing.EnrolledCount, existing.CreatedAt
		if err := repository.UpdateAcademyCourse(item); err != nil {
			return model.AcademyCourse{}, err
		}
	}
	// ponytail: covers stay public after they are replaced; release them like template files if storage matters.
	return item, markFilesPublic([]string{item.CoverURL})
}

// DeleteAcademyCourse refuses courses with enrollments: those hold paid records, unpublish them instead.
func DeleteAcademyCourse(id uint) error {
	total, err := repository.CountAcademyEnrollments(id)
	if err != nil {
		return err
	}
	if total > 0 {
		return safeMessageError{message: "课程已有报名，不能删除，可改为草稿下架"}
	}
	return repository.DeleteAcademyCourse(id)
}

func normalizeAcademyCourse(item *model.AcademyCourse) error {
	item.Slug, item.Title, item.Category = strings.TrimSpace(item.Slug), strings.TrimSpace(item.Title), strings.TrimSpace(item.Category)
	item.Summary, item.Body, item.CoverURL = strings.TrimSpace(item.Summary), strings.TrimSpace(item.Body), unsignedFileURL(item.CoverURL)
	if !templateSlugPattern.MatchString(item.Slug) {
		return safeMessageError{message: "标识只能使用 2-64 位小写字母、数字和 -"}
	}
	if item.Kind != model.AcademyKindCamp {
		item.Kind = model.AcademyKindCourse
	}
	if item.Status != model.AcademyStatusPublished {
		item.Status = model.AcademyStatusDraft
	}
	switch {
	case item.Title == "" || runeLen(item.Title) > 100:
		return safeMessageError{message: "标题需为 1-100 字"}
	case item.Category == "" || runeLen(item.Category) > 16:
		return safeMessageError{message: "分类需为 1-16 字"}
	case runeLen(item.Summary) > 300:
		return safeMessageError{message: "简介不能超过 300 字"}
	case runeLen(item.Body) > academyMaxBodyRunes:
		return safeMessageError{message: fmt.Sprintf("详情不能超过 %d 字", academyMaxBodyRunes)}
	case len(item.CoverURL) > 2048 || !templateImageURL(item.CoverURL):
		return safeMessageError{message: "封面地址无效"}
	case item.Capacity < 0 || item.Capacity > academyMaxCapacity:
		return safeMessageError{message: fmt.Sprintf("名额需为 0-%d（0 为不限）", academyMaxCapacity)}
	case math.IsNaN(item.FeeCredits) || item.FeeCredits < 0 || item.FeeCredits > academyMaxFeeCredits:
		return safeMessageError{message: fmt.Sprintf("报名费需为 0-%d", academyMaxFeeCredits)}
	case item.StartsAt != nil && item.EndsAt != nil && item.EndsAt.Before(*item.StartsAt):
		return safeMessageError{message: "结束时间不能早于开始时间"}
	case len(item.Lessons) > academyMaxLessons:
		return safeMessageError{message: fmt.Sprintf("课时最多 %d 节", academyMaxLessons)}
	}
	item.FeeCredits = math.Round(item.FeeCredits*100) / 100
	if item.Lessons == nil {
		item.Lessons = []model.AcademyLesson{}
	}
	for i := range item.Lessons {
		lesson := &item.Lessons[i]
		lesson.Title, lesson.VideoURL = strings.TrimSpace(lesson.Title), unsignedFileURL(lesson.VideoURL)
		if lesson.Title == "" || runeLen(lesson.Title) > 100 {
			return safeMessageError{message: fmt.Sprintf("第 %d 节课时标题需为 1-100 字", i+1)}
		}
		if len(lesson.VideoURL) > 2048 || !templateImageURL(lesson.VideoURL) {
			return safeMessageError{message: fmt.Sprintf("第 %d 节视频地址需为 http(s) 链接或上传的文件", i+1)}
		}
	}
	return nil
}

// AccountApp is one external application of the portal ("更多应用").
type AccountApp struct {
	ClientID  string `json:"clientId"`
	Name      string `json:"name"`
	LaunchURL string `json:"launchUrl"`
}

var appsCache ttlCache[[]AccountApp]

const appsCacheTTL = time.Minute

// AccountAppsForUser lists the new-api SSO applications the user may open, minus infinite itself.
func AccountAppsForUser(userID string) ([]AccountApp, error) {
	if !NewapiSSOEnabled() {
		return []AccountApp{}, nil
	}
	if cached, ok := appsCache.get(userID); ok {
		return cached, nil
	}
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ClientID  string `json:"client_id"`
		Name      string `json:"name"`
		LaunchURL string `json:"launch_url"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/sso/apps", nil, &raw); err != nil {
		return nil, err
	}
	apps := []AccountApp{}
	for _, app := range raw {
		if app.ClientID == config.Cfg.NewapiClientID || !strings.HasPrefix(app.LaunchURL, "https://") && !strings.HasPrefix(app.LaunchURL, "http://") {
			continue
		}
		apps = append(apps, AccountApp{ClientID: app.ClientID, Name: app.Name, LaunchURL: app.LaunchURL})
	}
	appsCache.set(userID, apps, appsCacheTTL)
	return apps, nil
}
