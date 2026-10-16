package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	bgmMaxTracks = 100
	bgmMaxBytes  = 20 << 20
)

// BgmTrackInput adds an uploaded audio file to the library; DurationSec is the browser's reading,
// replaced by ffprobe's when it is installed.
type BgmTrackInput struct {
	FileID      string   `json:"fileId"`
	Name        string   `json:"name"`
	Tags        []string `json:"tags"`
	DurationSec float64  `json:"durationSec"`
}

// BgmTracks lists the background music library.
func BgmTracks() ([]model.BgmTrack, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return nil, err
	}
	if settings.Private.BgmTracks == nil {
		return []model.BgmTrack{}, nil
	}
	return settings.Private.BgmTracks, nil
}

// isBgmTrack reports whether fileID is a library track.
func isBgmTrack(fileID string) bool {
	tracks, err := BgmTracks()
	return err == nil && slices.ContainsFunc(tracks, func(track model.BgmTrack) bool { return track.FileID == fileID })
}

// AddBgmTrack makes an audio file the admin uploaded a public library track.
func AddBgmTrack(ctx context.Context, input BgmTrackInput) ([]model.BgmTrack, error) {
	user, _ := UserFromContext(ctx)
	object, err := repository.GetStorageObject(input.FileID)
	if err != nil || !fileInUserSpace(user, object) || !strings.HasPrefix(object.MimeType, "audio/") || object.Bytes > bgmMaxBytes {
		return nil, safeMessageError{message: "请先上传 20MB 以内的音频文件"}
	}
	track := model.BgmTrack{FileID: object.ID, Name: strings.TrimSpace(input.Name), DurationSec: input.DurationSec, URL: "/api/files/" + object.ID + "/content"}
	if track.Name == "" || len([]rune(track.Name)) > 50 {
		return nil, safeMessageError{message: "曲目名称必填（≤50 字）"}
	}
	for _, tag := range input.Tags {
		if tag = strings.TrimSpace(tag); tag != "" && len([]rune(tag)) <= 20 && !slices.Contains(track.Tags, tag) && len(track.Tags) < 8 {
			track.Tags = append(track.Tags, tag)
		}
	}
	if seconds := probeAudioSeconds(ctx, object.ID); seconds > 0 {
		track.DurationSec = seconds
	}
	track.DurationSec = min(max(track.DurationSec, 0), 24*3600)
	if err := repository.SetStorageObjectsPublic([]string{object.ID}, true); err != nil {
		return nil, err
	}
	settings, err := repository.UpdateSettings(now(), func(stored *model.Settings) error {
		tracks := slices.DeleteFunc(stored.Private.BgmTracks, func(item model.BgmTrack) bool { return item.FileID == track.FileID })
		if len(tracks) >= bgmMaxTracks {
			return safeMessageError{message: "曲库最多 100 首"}
		}
		stored.Private.BgmTracks = append(tracks, track)
		return nil
	})
	return settings.Private.BgmTracks, err
}

// DeleteBgmTrack drops a track from the library; the file stays public for videos that already use it.
func DeleteBgmTrack(fileID string) ([]model.BgmTrack, error) {
	settings, err := repository.UpdateSettings(now(), func(stored *model.Settings) error {
		stored.Private.BgmTracks = slices.DeleteFunc(stored.Private.BgmTracks, func(item model.BgmTrack) bool { return item.FileID == fileID })
		return nil
	})
	if settings.Private.BgmTracks == nil {
		settings.Private.BgmTracks = []model.BgmTrack{}
	}
	return settings.Private.BgmTracks, err
}

// probeAudioSeconds reads a stored file's duration with ffprobe; 0 when ffprobe is missing or fails.
func probeAudioSeconds(ctx context.Context, fileID string) float64 {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	dir, err := os.MkdirTemp("", "bgm-*")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(dir)
	target := filepath.Join(dir, "audio")
	if saveStorageObjectFile(fileID, target) != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", target).Output()
	if err != nil {
		return 0
	}
	seconds, _ := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	return seconds
}
