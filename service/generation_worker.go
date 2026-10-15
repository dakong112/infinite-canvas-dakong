package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	generationTickInterval       = 3 * time.Second
	generationLease              = 2 * time.Minute
	generationLeaseRenewInterval = 30 * time.Second
	generationSettleWindow       = 100 * time.Second
	generationErrorMaxRunes      = 500
	generationPollInterval       = 5 * time.Second
	// generationPollLease delays a claimed poll's next run; long enough for a 200MB result download.
	generationPollLease    = 10 * time.Minute
	generationPollTimeout  = 30 * time.Minute
	generationPollsPerTick = 20
)

var (
	generationWorkerOnce sync.Once
	generationWake       = make(chan struct{}, 1)
	generationInFlight   atomic.Int64
	// generationJobCancels maps running job IDs to their *generationCancel so the cancel API stops them at once.
	generationJobCancels sync.Map
)

// generationCancel wraps a cancel func so sync.Map can CompareAndDelete it (funcs are not comparable).
type generationCancel struct{ cancel context.CancelFunc }

// GenerationJobResult is what a successful attempt produced.
type GenerationJobResult struct {
	OutputFileID string
	OutputURL    string
	OutputText   string
	Width        int
	Height       int
	// UpstreamTaskID means the job was submitted and now waits in "polling" for the upstream result.
	UpstreamTaskID string
}

// GenerationError is a failed attempt; Retryable asks the worker to try again after a backoff.
type GenerationError struct {
	Message   string
	Retryable bool
}

func (err GenerationError) Error() string {
	return err.Message
}

// GenerationExecuteFunc runs one claimed job; the handler package provides it.
type GenerationExecuteFunc func(ctx context.Context, job model.GenerationJob) (GenerationJobResult, error)

// GenerationPollFunc checks a submitted job once; done=false means the upstream is still working.
type GenerationPollFunc func(ctx context.Context, job model.GenerationJob) (result GenerationJobResult, done bool, err error)

func StartGenerationWorker(execute GenerationExecuteFunc, poll GenerationPollFunc) {
	generationWorkerOnce.Do(func() {
		go runGenerationWorker(execute, poll)
	})
}

// WakeGenerationWorker makes the worker look for jobs now instead of at the next tick.
func WakeGenerationWorker() {
	select {
	case generationWake <- struct{}{}:
	default:
	}
}

func runGenerationWorker(execute GenerationExecuteFunc, poll GenerationPollFunc) {
	ticker := time.NewTicker(generationTickInterval)
	defer ticker.Stop()
	for {
		generationWorkerTick(execute)
		pollGenerationJobs(poll)
		select {
		case <-ticker.C:
		case <-generationWake:
		}
	}
}

// generationWorkerTick never panics out: one bad tick must not stop the queue for the whole process.
func generationWorkerTick(execute GenerationExecuteFunc) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("generation worker tick panic: %v", recovered)
		}
	}()
	if err := repository.RecoverExpiredGenerationJobs(generationNow()); err != nil {
		log.Printf("recover generation jobs failed err=%v", err)
	}
	claimGenerationJobs(execute)
	settleTemplateFeesAsync()
}

// claimGenerationJobs fills free slots, keeping each user under their running limit.
// ponytail: the global limit is per process; share a counter if infinite runs several instances.
func claimGenerationJobs(execute GenerationExecuteFunc) {
	works := worksSetting()
	for generationInFlight.Load() < int64(works.GlobalConcurrency) {
		running, err := repository.RunningGenerationJobCounts()
		if err != nil {
			log.Printf("count running generation jobs failed err=%v", err)
			return
		}
		current := generationNow()
		var exclude repository.GenerationClaimExclusion
		for userID, count := range running {
			if count >= works.MemberConcurrency {
				exclude.Users = append(exclude.Users, userID)
			} else if count >= works.UserConcurrency {
				exclude.NonMemberUsers = append(exclude.NonMemberUsers, userID)
			}
		}
		job, err := repository.ClaimGenerationJob(current, uuid.NewString(), current.Add(generationLease), exclude, func(candidate model.GenerationJob) bool {
			limit := works.UserConcurrency
			if candidate.Member {
				limit = works.MemberConcurrency
			}
			return running[candidate.UserID] < limit
		})
		if err != nil {
			// A busy database (e.g. SQLite lock contention) just means nothing is claimed this tick.
			log.Printf("claim generation job failed, retrying next tick: %v", err)
			return
		}
		if job == nil {
			return
		}
		generationInFlight.Add(1)
		go runGenerationJob(execute, *job)
	}
}

func runGenerationJob(execute GenerationExecuteFunc, job model.GenerationJob) {
	defer func() {
		generationInFlight.Add(-1)
		WakeGenerationWorker()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entry := &generationCancel{cancel: cancel}
	generationJobCancels.Store(job.ID, entry)
	defer generationJobCancels.CompareAndDelete(job.ID, entry)
	go keepGenerationLease(ctx, cancel, job)

	result, runErr := safeExecuteGeneration(ctx, execute, job)
	if ctx.Err() != nil {
		// Cancelled by the user or the lease moved on; whoever changed the row owns it now.
		return
	}
	// The upstream call is done (and billed): keep trying to record it for most of the lease window, which
	// keeps renewing meanwhile, so a short database outage does not make the job run (and bill) again.
	// ponytail: a job left running after the window is re-run when its lease expires, which can bill twice.
	deadline := time.Now().Add(generationSettleWindow)
	for attempt := 1; ; attempt++ {
		ok, err := settleGenerationJob(job, result, runErr, generationNow())
		if err == nil {
			if !ok {
				log.Printf("generation job lease lost before settle id=%s", job.ID)
			}
			return
		}
		log.Printf("settle generation job failed id=%s attempt=%d err=%v", job.ID, attempt, err)
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(min(time.Duration(attempt)*time.Second, 10*time.Second))
	}
}

// safeExecuteGeneration turns a panic in the executor into a final failure instead of crashing the process.
func safeExecuteGeneration(ctx context.Context, execute GenerationExecuteFunc, job model.GenerationJob) (result GenerationJobResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("generation job panic: id=%s %v", job.ID, recovered)
			result, err = GenerationJobResult{}, GenerationError{Message: "生成失败"}
		}
	}()
	return execute(ctx, job)
}

func keepGenerationLease(ctx context.Context, cancel context.CancelFunc, job model.GenerationJob) {
	ticker := time.NewTicker(generationLeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := repository.RenewGenerationLease(job.ID, job.LeaseToken, generationNow().Add(generationLease))
			if err != nil {
				log.Printf("renew generation lease failed id=%s err=%v", job.ID, err)
				continue
			}
			if !ok {
				cancel()
				return
			}
		}
	}
}

// settleGenerationJob records one finished attempt: success, a delayed retry, or a final failure.
func settleGenerationJob(job model.GenerationJob, result GenerationJobResult, runErr error, now time.Time) (bool, error) {
	values := map[string]any{"lease_token": "", "lease_expires_at": nil}
	if runErr == nil && result.UpstreamTaskID != "" {
		// Submitted (and billed): from now on the job is only polled, never submitted again.
		values["status"] = model.GenerationStatusPolling
		values["upstream_task_id"] = result.UpstreamTaskID
		values["next_run_at"] = now.Add(generationPollInterval)
		values["poll_deadline"] = now.Add(generationPollTimeout)
		values["error"] = ""
		return repository.FinishGenerationJob(job.ID, job.LeaseToken, values)
	}
	if runErr == nil {
		values["status"] = model.GenerationStatusSucceeded
		values["error"] = ""
		values["output_file_id"] = result.OutputFileID
		values["output_url"] = result.OutputURL
		values["output_text"] = result.OutputText
		values["width"] = result.Width
		values["height"] = result.Height
		values["finished_at"] = now
		return repository.FinishGenerationJob(job.ID, job.LeaseToken, values)
	}
	var failure GenerationError
	if !errors.As(runErr, &failure) {
		log.Printf("generation job failed id=%s err=%v", job.ID, runErr)
		failure = GenerationError{Message: "生成失败，请重试"}
	}
	values["error"] = truncateGenerationError(failure.Message)
	if failure.Retryable && job.Attempts < job.MaxAttempts {
		values["status"] = model.GenerationStatusQueued
		values["next_run_at"] = now.Add(generationBackoff(job.Attempts))
		return repository.FinishGenerationJob(job.ID, job.LeaseToken, values)
	}
	values["status"] = model.GenerationStatusFailed
	values["finished_at"] = now
	return repository.FinishGenerationJob(job.ID, job.LeaseToken, values)
}

// truncateGenerationError keeps upstream error bodies from overflowing the error column.
func truncateGenerationError(message string) string {
	if runes := []rune(message); len(runes) > generationErrorMaxRunes {
		return string(runes[:generationErrorMaxRunes])
	}
	return message
}

// generationBackoff is 15s, 30s, 60s … capped at 5 minutes.
func generationBackoff(attempts int) time.Duration {
	return min(15*time.Second<<min(max(attempts-1, 0), 5), 5*time.Minute)
}

// pollGenerationJobs checks due polling jobs; no lease or concurrency slot is held between polls.
func pollGenerationJobs(poll GenerationPollFunc) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("generation poll tick panic: %v", recovered)
		}
	}()
	current := generationNow()
	jobs, err := repository.ClaimPollingGenerationJobs(current, current.Add(generationPollLease), generationPollsPerTick)
	if err != nil {
		log.Printf("claim polling generation jobs failed err=%v", err)
	}
	for _, job := range jobs {
		go runGenerationPoll(poll, job)
	}
}

func runGenerationPoll(poll GenerationPollFunc, job model.GenerationJob) {
	result, done, err := func() (result GenerationJobResult, done bool, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("generation poll panic: id=%s %v", job.ID, recovered)
				err = GenerationError{Message: "生成失败"}
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), generationPollLease-time.Minute)
		defer cancel()
		return poll(ctx, job)
	}()
	if _, err := settlePolledGenerationJob(job, result, done, err, generationNow()); err != nil {
		log.Printf("settle polled generation job failed id=%s err=%v", job.ID, err)
	}
}

// settlePolledGenerationJob records one poll: success, another poll later, or failure (final error or deadline).
func settlePolledGenerationJob(job model.GenerationJob, result GenerationJobResult, done bool, pollErr error, now time.Time) (bool, error) {
	var failure GenerationError
	if pollErr != nil && !errors.As(pollErr, &failure) {
		log.Printf("generation poll failed id=%s err=%v", job.ID, pollErr)
		failure = GenerationError{Message: "视频结果查询失败", Retryable: true}
	}
	expired := job.PollDeadline != nil && !now.Before(*job.PollDeadline)
	switch {
	case pollErr == nil && done:
		return repository.UpdatePollingGenerationJob(job.ID, map[string]any{
			"status": model.GenerationStatusSucceeded, "error": "", "output_file_id": result.OutputFileID, "output_url": result.OutputURL,
			"output_text": result.OutputText, "width": result.Width, "height": result.Height, "finished_at": now,
		})
	case pollErr != nil && (!failure.Retryable || expired):
		return repository.UpdatePollingGenerationJob(job.ID, map[string]any{"status": model.GenerationStatusFailed, "error": truncateGenerationError(failure.Message), "finished_at": now})
	case expired:
		return repository.UpdatePollingGenerationJob(job.ID, map[string]any{"status": model.GenerationStatusFailed, "error": "视频生成超时", "finished_at": now})
	}
	values := map[string]any{"next_run_at": now.Add(generationPollInterval)}
	if pollErr != nil {
		values["error"] = truncateGenerationError(failure.Message)
	}
	return repository.UpdatePollingGenerationJob(job.ID, values)
}
