package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"gorm.io/gorm"
)

// runGenerationTestIsolated re-runs name in a child process so repository.DB() starts fresh.
// It returns true in the parent, which must then return.
func runGenerationTestIsolated(t *testing.T, name string) bool {
	t.Helper()
	marker := "GENERATION_CHILD_" + name
	if os.Getenv(marker) == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+name+"$", "-test.v", "-test.timeout=110s")
	cmd.Env = append(os.Environ(), marker+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated %s: %v\n%s", name, err, output)
	}
	t.Logf("%s", output)
	return true
}

// generationTestDB opens the test database. GENERATION_TEST_DRIVER/GENERATION_TEST_DSN point it at a
// dedicated, empty MySQL or PostgreSQL database; the generation, storage and settings tables are wiped.
func generationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	driver, dsn := os.Getenv("GENERATION_TEST_DRIVER"), os.Getenv("GENERATION_TEST_DSN")
	if driver == "" {
		driver, dsn = "sqlite", ":memory:"
	}
	config.Cfg = config.Config{StorageDriver: driver, DatabaseDSN: dsn, AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1, LocalStorageDir: t.TempDir()}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	if driver == "sqlite" {
		connection, _ := db.DB()
		connection.SetMaxOpenConns(1)
	}
	for _, table := range []any{&model.GenerationJob{}, &model.GenerationBatch{}, &model.StorageObject{}, &model.Setting{}} {
		if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func queuedTestJob(id string, userID string, createdAt time.Time) model.GenerationJob {
	return model.GenerationJob{
		ID: id, UserID: userID, BatchID: "batch-test", Source: model.GenerationSourceImage, Kind: model.GenerationKindImage,
		Endpoint: "/images/generations", Model: "gpt-image-1", RequestJSON: `{"model":"gpt-image-1","prompt":"p","n":1}`,
		InputFileIDs: []string{}, Status: model.GenerationStatusQueued, MaxAttempts: 3,
		NextRunAt: createdAt, CreatedAt: createdAt, ExpiresAt: createdAt.Add(7 * 24 * time.Hour),
	}
}

func loadTestJob(t *testing.T, db *gorm.DB, id string) model.GenerationJob {
	t.Helper()
	var job model.GenerationJob
	if err := db.First(&job, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func TestGenerationQueueRepository(t *testing.T) {
	if runGenerationTestIsolated(t, "TestGenerationQueueRepository") {
		return
	}
	db := generationTestDB(t)
	now := generationNow()
	jobs := make([]model.GenerationJob, 0, 12)
	for i := range 12 {
		jobs = append(jobs, queuedTestJob(fmt.Sprintf("job-%02d", i), fmt.Sprintf("u%d", i%3), now.Add(time.Duration(i)*time.Millisecond)))
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	acceptAll := func(model.GenerationJob) bool { return true }

	t.Run("concurrent claims never share a job", func(t *testing.T) {
		var mu sync.Mutex
		claimed := map[string]int{}
		var wg sync.WaitGroup
		for worker := range 4 {
			wg.Go(func() {
				for {
					job, err := repository.ClaimGenerationJob(now.Add(time.Second), fmt.Sprintf("lease-%d", worker), now.Add(time.Minute), repository.GenerationClaimExclusion{}, acceptAll)
					if err != nil {
						t.Error(err)
						return
					}
					if job == nil {
						return
					}
					if job.Status != model.GenerationStatusRunning || job.Attempts != 1 {
						t.Errorf("claimed job state: %#v", job)
					}
					mu.Lock()
					claimed[job.ID]++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if len(claimed) != len(jobs) {
			t.Fatalf("claimed %d distinct jobs, want %d: %v", len(claimed), len(jobs), claimed)
		}
		for id, count := range claimed {
			if count != 1 {
				t.Fatalf("job %s claimed %d times", id, count)
			}
		}
		counts, err := repository.RunningGenerationJobCounts()
		if err != nil {
			t.Fatal(err)
		}
		if counts["u0"] != 4 || counts["u1"] != 4 || counts["u2"] != 4 {
			t.Fatalf("running counts = %v", counts)
		}
	})

	t.Run("claim respects due time and the allow filter", func(t *testing.T) {
		if err := db.Model(&model.GenerationJob{}).Where("1 = 1").Updates(map[string]any{"status": model.GenerationStatusQueued, "lease_token": "", "attempts": 0}).Error; err != nil {
			t.Fatal(err)
		}
		if job, err := repository.ClaimGenerationJob(now.Add(-time.Hour), "early", now.Add(time.Minute), repository.GenerationClaimExclusion{}, acceptAll); err != nil || job != nil {
			t.Fatalf("jobs not yet due were claimed: %#v %v", job, err)
		}
		job, err := repository.ClaimGenerationJob(now.Add(time.Second), "only-u2", now.Add(time.Minute), repository.GenerationClaimExclusion{}, func(candidate model.GenerationJob) bool { return candidate.UserID == "u2" })
		if err != nil || job == nil || job.UserID != "u2" {
			t.Fatalf("allow filter ignored: %#v %v", job, err)
		}
	})

	t.Run("lease renewal and finish need the current token", func(t *testing.T) {
		job, err := repository.ClaimGenerationJob(now.Add(time.Second), "lease-current", now.Add(time.Minute), repository.GenerationClaimExclusion{}, acceptAll)
		if err != nil || job == nil {
			t.Fatalf("claim: %#v %v", job, err)
		}
		if ok, err := repository.RenewGenerationLease(job.ID, "stale", now.Add(2*time.Minute)); err != nil || ok {
			t.Fatalf("stale token renewed: %v %v", ok, err)
		}
		if ok, err := repository.RenewGenerationLease(job.ID, "lease-current", now.Add(2*time.Minute)); err != nil || !ok {
			t.Fatalf("current token not renewed: %v %v", ok, err)
		}
		if ok, err := repository.FinishGenerationJob(job.ID, "stale", map[string]any{"status": model.GenerationStatusSucceeded}); err != nil || ok {
			t.Fatalf("stale token finished the job: %v %v", ok, err)
		}
		if ok, err := repository.FinishGenerationJob(job.ID, "lease-current", map[string]any{"status": model.GenerationStatusSucceeded, "lease_token": ""}); err != nil || !ok {
			t.Fatalf("current token could not finish: %v %v", ok, err)
		}
		if loaded := loadTestJob(t, db, job.ID); loaded.Status != model.GenerationStatusSucceeded {
			t.Fatalf("status = %s", loaded.Status)
		}
	})

	t.Run("expired leases are requeued until attempts run out", func(t *testing.T) {
		expired := now.Add(-time.Minute)
		retry := queuedTestJob("lease-retry", "u1", now)
		retry.Status, retry.Attempts, retry.LeaseToken, retry.LeaseExpiresAt = model.GenerationStatusRunning, 1, "dead-worker", &expired
		final := queuedTestJob("lease-final", "u1", now)
		final.Status, final.Attempts, final.LeaseToken, final.LeaseExpiresAt = model.GenerationStatusRunning, 3, "dead-worker", &expired
		if err := db.Create(&[]model.GenerationJob{retry, final}).Error; err != nil {
			t.Fatal(err)
		}
		if err := repository.RecoverExpiredGenerationJobs(now); err != nil {
			t.Fatal(err)
		}
		if loaded := loadTestJob(t, db, "lease-retry"); loaded.Status != model.GenerationStatusQueued || loaded.LeaseToken != "" || loaded.LeaseExpiresAt != nil {
			t.Fatalf("expired job not requeued: %#v", loaded)
		}
		if loaded := loadTestJob(t, db, "lease-final"); loaded.Status != model.GenerationStatusFailed || loaded.FinishedAt == nil || loaded.Error == "" {
			t.Fatalf("exhausted job not failed: %#v", loaded)
		}
		// job-02 was claimed by "only-u2" above and still holds a lease until now+1min.
		if loaded := loadTestJob(t, db, "job-02"); loaded.Status != model.GenerationStatusRunning || loaded.LeaseToken != "only-u2" {
			t.Fatalf("jobs with live leases must not be touched: %#v", loaded)
		}
	})
}

var testPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

// saveGenerationTestSettings opens one remote channel serving the test models to normal users.
func saveGenerationTestSettings(t *testing.T, public model.PublicSetting, models ...string) {
	t.Helper()
	enabled := true
	public.ModelChannel.AllowUserRemoteChannel = &enabled
	settings := model.Settings{
		Public:  public,
		Private: model.PrivateSetting{Channels: []model.ModelChannel{{ID: "c1", Protocol: "openai", Name: "test", BaseURL: "http://127.0.0.1:9", APIKey: "k", Models: models, Weight: 1, Enabled: true}}},
	}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func claimTestJob(t *testing.T, id string, at time.Time) model.GenerationJob {
	t.Helper()
	job, err := repository.ClaimGenerationJob(at, "lease-"+id, at.Add(time.Minute), repository.GenerationClaimExclusion{}, func(candidate model.GenerationJob) bool { return candidate.ID == id })
	if err != nil || job == nil {
		t.Fatalf("claim %s: %#v %v", id, job, err)
	}
	return *job
}

func TestGenerationJobsService(t *testing.T) {
	if runGenerationTestIsolated(t, "TestGenerationJobsService") {
		return
	}
	db := generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{}, "gpt-image-1", "gpt-4o")
	u1 := model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	u2 := model.AuthUser{ID: "u2", Role: model.UserRoleUser}
	ctx1, ctx2 := WithUser(context.Background(), u1), WithUser(context.Background(), u2)
	input, err := UploadStorageObject(ctx1, "product.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	editItem := GenerationJobInput{Kind: model.GenerationKindImage, Endpoint: "/images/edits", Model: "gpt-image-1",
		Request: map[string]any{"prompt": "白底图", "n": 4, "size": "1024x1024"}, InputFileIDs: []string{input.ID}, Count: 2}
	before := generationNow()
	_, jobs, err := CreateGenerationBatch(ctx1, u1, GenerationBatchInput{Title: "测试", Source: model.GenerationSourceImage, Items: []GenerationJobInput{editItem}})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].ChannelID != "c1" || jobs[0].Prompt != "白底图" || jobs[0].Status != model.GenerationStatusQueued || jobs[0].MaxAttempts != 3 {
		t.Fatalf("created jobs: %#v", jobs)
	}
	if !strings.Contains(jobs[0].RequestJSON, `"n":1`) || !strings.Contains(jobs[0].RequestJSON, `"model":"gpt-image-1"`) {
		t.Fatalf("image jobs must request exactly one image: %s", jobs[0].RequestJSON)
	}
	if retention := jobs[0].ExpiresAt.Sub(before); retention < 7*24*time.Hour || retention > 7*24*time.Hour+time.Minute {
		t.Fatalf("non-member retention = %v, want 7 days", retention)
	}

	t.Run("rejects foreign files, inline images and unknown endpoints", func(t *testing.T) {
		cases := map[string]GenerationJobInput{
			"输入图片不存在":            editItem,
			"请求参数过大，图片请通过输入文件提交": {Kind: model.GenerationKindImage, Endpoint: "/images/generations", Model: "gpt-image-1", Request: map[string]any{"prompt": "data:image/png;base64,AAAA"}},
			"不支持的任务类型":           {Kind: model.GenerationKindImage, Endpoint: "/videos", Model: "gpt-image-1", Request: map[string]any{"prompt": "x"}},
			"输入图片引用无效":           {Kind: model.GenerationKindVision, Endpoint: "/responses", Model: "gpt-4o", Request: map[string]any{"input": "input-file:someone-else"}},
		}
		for want, item := range cases {
			_, _, err := CreateGenerationBatch(ctx2, u2, GenerationBatchInput{Source: model.GenerationSourceImage, Items: []GenerationJobInput{item}})
			if err == nil || err.Error() != want {
				t.Errorf("want %q, got %v", want, err)
			}
		}
	})

	t.Run("request builder replays stored input files", func(t *testing.T) {
		body, contentType, err := BuildGenerationRequest(jobs[0])
		if err != nil {
			t.Fatal(err)
		}
		_, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			t.Fatal(err)
		}
		form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			t.Fatal(err)
		}
		if form.Value["prompt"][0] != "白底图" || form.Value["n"][0] != "1" || form.Value["model"][0] != "gpt-image-1" || form.Value["size"][0] != "1024x1024" {
			t.Fatalf("multipart fields = %v", form.Value)
		}
		files := form.File["image"]
		if len(files) != 1 || files[0].Header.Get("Content-Type") != "image/png" {
			t.Fatalf("multipart files = %#v", files)
		}
		file, _ := files[0].Open()
		data, _ := io.ReadAll(file)
		_ = file.Close()
		if !bytes.Equal(data, testPNG) {
			t.Fatal("replayed image bytes differ from the stored input")
		}

		_, visionJobs, err := CreateGenerationBatch(ctx1, u1, GenerationBatchInput{Source: model.GenerationSourceImage, Items: []GenerationJobInput{{
			Kind: model.GenerationKindVision, Endpoint: "/responses", Model: "gpt-4o", InputFileIDs: []string{input.ID},
			Request: map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "看图"},
				map[string]any{"type": "input_image", "image_url": "input-file:" + input.ID},
			}}}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		jsonBody, jsonType, err := BuildGenerationRequest(visionJobs[0])
		if err != nil || jsonType != "application/json" {
			t.Fatalf("json request: %v %s", err, jsonType)
		}
		if !strings.Contains(string(jsonBody), `"data:image/png;base64,`+base64.StdEncoding.EncodeToString(testPNG)+`"`) || strings.Contains(string(jsonBody), "input-file:") {
			t.Fatalf("file reference not replaced: %s", jsonBody)
		}
	})

	output, err := UploadStorageObject(ctx1, "", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	now := generationNow()

	t.Run("settle retries transient failures and finalizes the rest", func(t *testing.T) {
		job := claimTestJob(t, jobs[0].ID, now)
		if ok, err := settleGenerationJob(job, GenerationJobResult{}, GenerationError{Message: "upstream busy", Retryable: true}, now); err != nil || !ok {
			t.Fatalf("settle retryable: %v %v", ok, err)
		}
		requeued := loadTestJob(t, db, job.ID)
		if requeued.Status != model.GenerationStatusQueued || requeued.Error != "upstream busy" || !requeued.NextRunAt.Equal(now.Add(15*time.Second)) || requeued.LeaseToken != "" {
			t.Fatalf("retryable failure not requeued with backoff: %#v", requeued)
		}
		job = claimTestJob(t, jobs[0].ID, now.Add(16*time.Second))
		if ok, err := settleGenerationJob(job, GenerationJobResult{}, GenerationError{Message: "content policy"}, now); err != nil || !ok {
			t.Fatalf("settle final: %v %v", ok, err)
		}
		if failed := loadTestJob(t, db, job.ID); failed.Status != model.GenerationStatusFailed || failed.Attempts != 2 || failed.FinishedAt == nil {
			t.Fatalf("non-retryable failure: %#v", failed)
		}
		if _, err := RetryGenerationJob("u2", job.ID); err == nil {
			t.Fatal("another user retried the job")
		}
		retried, err := RetryGenerationJob("u1", job.ID)
		if err != nil || retried.Status != model.GenerationStatusQueued || retried.Attempts != 0 || retried.Error != "" {
			t.Fatalf("retry: %#v %v", retried, err)
		}
		job = claimTestJob(t, jobs[0].ID, now.Add(17*time.Second))
		result := GenerationJobResult{OutputFileID: output.ID, OutputURL: output.URL, Width: 1, Height: 1}
		if ok, err := settleGenerationJob(job, result, nil, now); err != nil || !ok {
			t.Fatalf("settle success: %v %v", ok, err)
		}
		if done := loadTestJob(t, db, job.ID); done.Status != model.GenerationStatusSucceeded || done.OutputFileID != output.ID || done.Width != 1 {
			t.Fatalf("success not recorded: %#v", done)
		}

		if err := db.Model(&model.GenerationJob{}).Where("id = ?", jobs[1].ID).Update("max_attempts", 1).Error; err != nil {
			t.Fatal(err)
		}
		last := claimTestJob(t, jobs[1].ID, now)
		if ok, err := settleGenerationJob(last, GenerationJobResult{}, GenerationError{Message: "timeout", Retryable: true}, now); err != nil || !ok {
			t.Fatalf("settle exhausted: %v %v", ok, err)
		}
		if exhausted := loadTestJob(t, db, last.ID); exhausted.Status != model.GenerationStatusFailed || exhausted.Error != "timeout" {
			t.Fatalf("retryable failure at max attempts must fail: %#v", exhausted)
		}
	})

	t.Run("jobs and works are private to their owner", func(t *testing.T) {
		ids := []string{jobs[0].ID, jobs[1].ID}
		if found, err := GenerationJobsByIDs("u2", ids); err != nil || len(found) != 0 {
			t.Fatalf("u2 read u1 jobs: %v %v", found, err)
		}
		if found, err := BatchGenerationJobs("u2", jobs[0].BatchID); err != nil || len(found) != 0 {
			t.Fatalf("u2 read u1 batch: %v %v", found, err)
		}
		if page, err := ListWorks("u2", WorksQuery{}); err != nil || len(page.Items) != 0 {
			t.Fatalf("u2 works: %#v %v", page, err)
		}
		if err := DeleteWork(ctx2, "u2", jobs[0].ID); err == nil {
			t.Fatal("u2 deleted u1 work")
		}
		page, err := ListWorks("u1", WorksQuery{})
		if err != nil || len(page.Items) != 2 || page.Counts.Succeeded != 1 || page.Counts.Failed != 1 || page.Counts.Running != 0 {
			t.Fatalf("u1 works: %#v %v", page, err)
		}
		failed, err := ListWorks("u1", WorksQuery{Status: "failed"})
		if err != nil || len(failed.Items) != 1 || failed.Items[0].ID != jobs[1].ID {
			t.Fatalf("status filter: %#v %v", failed, err)
		}
		first, err := ListWorks("u1", WorksQuery{Limit: 1})
		if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatalf("first page: %#v %v", first, err)
		}
		second, err := ListWorks("u1", WorksQuery{Limit: 1, Cursor: first.NextCursor})
		if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
			t.Fatalf("second page: %#v %v", second, err)
		}

		_, pending, err := CreateGenerationBatch(ctx1, u1, GenerationBatchInput{Source: model.GenerationSourceImage, Items: []GenerationJobInput{{
			Kind: model.GenerationKindImage, Endpoint: "/images/generations", Model: "gpt-image-1", Request: map[string]any{"prompt": "取消我"},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := CancelGenerationJob("u2", pending[0].ID); err == nil {
			t.Fatal("u2 cancelled u1 job")
		}
		cancelled, err := CancelGenerationJob("u1", pending[0].ID)
		if err != nil || cancelled.Status != model.GenerationStatusCancelled {
			t.Fatalf("cancel: %#v %v", cancelled, err)
		}
	})

	t.Run("retention cleanup removes expired outputs", func(t *testing.T) {
		if err := db.Model(&model.GenerationJob{}).Where("id = ?", jobs[0].ID).Update("expires_at", now.Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		cleanupExpiredWorks()
		if expired := loadTestJob(t, db, jobs[0].ID); expired.DeletedAt == nil {
			t.Fatal("expired work not deleted")
		}
		if _, err := DownloadStorageObject(output.ID, ""); err == nil {
			t.Fatal("expired output file still downloadable")
		}
		if err := DeleteWork(ctx1, "u1", jobs[1].ID); err != nil {
			t.Fatal(err)
		}
		page, err := ListWorks("u1", WorksQuery{})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if item.ID == jobs[0].ID || item.ID == jobs[1].ID {
				t.Fatalf("deleted work still listed: %s", item.ID)
			}
		}
	})
}

func TestGenerationClaimFairness(t *testing.T) {
	if runGenerationTestIsolated(t, "TestGenerationClaimFairness") {
		return
	}
	db := generationTestDB(t)
	now := generationNow()
	jobs := make([]model.GenerationJob, 0, 26)
	for i := range 25 {
		jobs = append(jobs, queuedTestJob(fmt.Sprintf("a-%02d", i), "userA", now.Add(time.Duration(i)*time.Millisecond)))
	}
	jobs = append(jobs, queuedTestJob("b-00", "userB", now.Add(time.Second)))
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	// userA is at its cap, so its 25 older queued jobs must not hide userB's single job.
	job, err := repository.ClaimGenerationJob(now.Add(2*time.Second), "fair", now.Add(time.Minute), repository.GenerationClaimExclusion{Users: []string{"userA"}}, func(model.GenerationJob) bool { return true })
	if err != nil || job == nil || job.UserID != "userB" {
		t.Fatalf("userB starved behind userA: %#v %v", job, err)
	}
	// Non-member cap only blocks non-member jobs.
	member := queuedTestJob("a-member", "userA", now.Add(2*time.Second))
	member.Member = true
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	job, err = repository.ClaimGenerationJob(now.Add(3*time.Second), "fair2", now.Add(time.Minute), repository.GenerationClaimExclusion{NonMemberUsers: []string{"userA"}}, func(model.GenerationJob) bool { return true })
	if err != nil || job == nil || job.ID != "a-member" {
		t.Fatalf("member job of a non-member-capped user should run: %#v %v", job, err)
	}
	if got := truncateGenerationError(strings.Repeat("错", 5000)); len([]rune(got)) != generationErrorMaxRunes {
		t.Fatalf("error not truncated: %d", len([]rune(got)))
	}
}

func TestGenerationLimitsAndPanicRecovery(t *testing.T) {
	if runGenerationTestIsolated(t, "TestGenerationLimitsAndPanicRecovery") {
		return
	}
	db := generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{}, "gpt-image-1")
	user := model.AuthUser{ID: "u9", Role: model.UserRoleUser}
	ctx := WithUser(context.Background(), user)
	item := GenerationJobInput{Kind: model.GenerationKindImage, Endpoint: "/images/generations", Model: "gpt-image-1", Request: map[string]any{"prompt": "x"}}

	for _, count := range []int{math.MaxInt64, math.MaxInt, 21, -1} {
		big := item
		big.Count = count
		if _, _, err := CreateGenerationBatch(ctx, user, GenerationBatchInput{Source: model.GenerationSourceImage, Items: []GenerationJobInput{big}}); err == nil {
			t.Fatalf("count %d accepted", count)
		}
	}
	var count int64
	if db.Model(&model.GenerationJob{}).Count(&count); count != 0 {
		t.Fatalf("rejected batches left %d jobs", count)
	}

	full := item
	full.Count = 20
	for range 5 {
		if _, _, err := CreateGenerationBatch(ctx, user, GenerationBatchInput{Source: model.GenerationSourceImage, Items: []GenerationJobInput{full}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := CreateGenerationBatch(ctx, user, GenerationBatchInput{Source: model.GenerationSourceImage, Items: []GenerationJobInput{item}}); err == nil {
		t.Fatal("per-user queue cap not enforced")
	}

	var job model.GenerationJob
	if err := db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.GenerationJob{}).Where("id = ?", job.ID).Update("next_run_at", generationNow().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	claimed := claimTestJob(t, job.ID, generationNow())
	generationInFlight.Add(1)
	runGenerationJob(func(context.Context, model.GenerationJob) (GenerationJobResult, error) { panic("boom") }, claimed)
	if failed := loadTestJob(t, db, job.ID); failed.Status != model.GenerationStatusFailed || failed.Error != "生成失败" {
		t.Fatalf("panicking executor not settled as a final failure: %#v", failed)
	}
}

func TestGenerationBatchContextRepository(t *testing.T) {
	if runGenerationTestIsolated(t, "TestGenerationBatchContextRepository") {
		return
	}
	generationTestDB(t)
	now := generationNow()
	if err := repository.CreateGenerationBatch(model.GenerationBatch{ID: "b1", UserID: "u1", Source: model.GenerationSourceEcommerceSet, ContextJSON: "{}", CreatedAt: now}, nil); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repository.GetUserGenerationBatch("u2", "b1"); err != nil || found {
		t.Fatalf("other user found batch: %v %v", found, err)
	}
	batch, found, err := repository.GetUserGenerationBatch("u1", "b1")
	if err != nil || !found || batch.Version != 0 {
		t.Fatalf("get batch = %#v %v %v", batch, found, err)
	}
	if ok, err := repository.UpdateGenerationBatchContext("u1", "b1", 0, `{"a":1}`); err != nil || !ok {
		t.Fatalf("first update = %v %v", ok, err)
	}
	if ok, err := repository.UpdateGenerationBatchContext("u1", "b1", 0, `{"a":2}`); err != nil || ok {
		t.Fatalf("stale update = %v %v", ok, err)
	}
	if ok, err := repository.UpdateGenerationBatchContext("u2", "b1", 1, `{"a":3}`); err != nil || ok {
		t.Fatalf("other user update = %v %v", ok, err)
	}
	batch, _, _ = repository.GetUserGenerationBatch("u1", "b1")
	if batch.Version != 1 || batch.ContextJSON != `{"a":1}` {
		t.Fatalf("after updates = %#v", batch)
	}
	job := queuedTestJob("j1", "u1", now)
	job.BatchID = "b1"
	if err := repository.CreateGenerationJobs([]model.GenerationJob{job}); err != nil {
		t.Fatal(err)
	}
	if jobs, err := repository.ListUserBatchGenerationJobs("u1", "b1"); err != nil || len(jobs) != 1 {
		t.Fatalf("batch jobs = %v %v", jobs, err)
	}
	if err := repository.CreateGenerationBatch(model.GenerationBatch{ID: "b2", UserID: "u1", Source: model.GenerationSourceEcommerceSet, CreatedAt: now.Add(time.Second)}, nil); err != nil {
		t.Fatal(err)
	}
	batches, err := repository.ListUserGenerationBatches("u1", model.GenerationSourceEcommerceSet, 50)
	if err != nil || len(batches) != 2 || batches[0].ID != "b2" {
		t.Fatalf("list batches = %#v %v", batches, err)
	}
}
