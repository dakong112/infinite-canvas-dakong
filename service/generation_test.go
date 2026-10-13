package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
					job, err := repository.ClaimGenerationJob(now.Add(time.Second), fmt.Sprintf("lease-%d", worker), now.Add(time.Minute), acceptAll)
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
		if job, err := repository.ClaimGenerationJob(now.Add(-time.Hour), "early", now.Add(time.Minute), acceptAll); err != nil || job != nil {
			t.Fatalf("jobs not yet due were claimed: %#v %v", job, err)
		}
		job, err := repository.ClaimGenerationJob(now.Add(time.Second), "only-u2", now.Add(time.Minute), func(candidate model.GenerationJob) bool { return candidate.UserID == "u2" })
		if err != nil || job == nil || job.UserID != "u2" {
			t.Fatalf("allow filter ignored: %#v %v", job, err)
		}
	})

	t.Run("lease renewal and finish need the current token", func(t *testing.T) {
		job, err := repository.ClaimGenerationJob(now.Add(time.Second), "lease-current", now.Add(time.Minute), acceptAll)
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
