package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
)

const (
	// GenerationMaxMediaBytes caps one stored video or audio result (and one compose input).
	GenerationMaxMediaBytes = 200 << 20
	composeFPS              = 30
	composeBgmVolume        = 0.15
	composeMaxShots         = 30
	composeMaxSeconds       = 120
	composeMaxSide          = 1920
	composeMinSide          = 256
	composeTimeout          = 15 * time.Minute
)

// composeSlots bounds concurrent ffmpeg runs (env COMPOSE_CONCURRENCY, default 1): each one is CPU heavy.
var composeSlots = make(chan struct{}, composeConcurrency())

func composeConcurrency() int {
	if value, err := strconv.Atoi(os.Getenv("COMPOSE_CONCURRENCY")); err == nil && value > 0 {
		return value
	}
	return 1
}

// ComposeShot is one clip of a compose job: the shot video, its optional voiceover starting with
// the shot, and the subtitle shown while it plays. Seconds is the shot's length in the final cut.
type ComposeShot struct {
	VideoFileID string  `json:"video"`
	AudioFileID string  `json:"audio,omitempty"`
	Seconds     float64 `json:"seconds"`
	Subtitle    string  `json:"subtitle,omitempty"`
}

// ComposeRequest is the RequestJSON of a compose job.
type ComposeRequest struct {
	Width     int           `json:"width"`
	Height    int           `json:"height"`
	Shots     []ComposeShot `json:"shots"`
	BgmFileID string        `json:"bgm,omitempty"`
	Subtitles bool          `json:"subtitles,omitempty"`
}

// ComposeOutput is the OutputText of a compose job that asked for subtitles.
type ComposeOutput struct {
	SubtitleFileID  string `json:"subtitleFileId"`
	SubtitleURL     string `json:"subtitleUrl"`
	SubtitlesBurned bool   `json:"subtitlesBurned"`
}

// ComposeGenerationJob runs ffmpeg: every shot is scaled and padded to the output size at a fixed
// frame rate and cut (or held on its last frame) to its planned length, the clips are joined, each
// voiceover starts with its shot, background music loops quietly underneath, and subtitles are
// burned in when a CJK font is installed (the .srt is stored either way).
func ComposeGenerationJob(ctx context.Context, user model.AuthUser, job model.GenerationJob) (GenerationJobResult, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return GenerationJobResult{}, GenerationError{Message: "服务器未安装 ffmpeg"}
	}
	var request ComposeRequest
	if err := json.Unmarshal([]byte(job.RequestJSON), &request); err != nil || len(request.Shots) == 0 || request.Width <= 0 || request.Height <= 0 {
		return GenerationJobResult{}, GenerationError{Message: "任务参数已损坏"}
	}
	select {
	case composeSlots <- struct{}{}:
		defer func() { <-composeSlots }()
	case <-ctx.Done():
		return GenerationJobResult{}, GenerationError{Message: "合成排队已取消", Retryable: true}
	}
	ctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "compose-")
	if err != nil {
		return GenerationJobResult{}, GenerationError{Message: "视频合成失败", Retryable: true}
	}
	defer os.RemoveAll(dir)
	type fetchedInput struct{ name, format string }
	fetched := map[string]fetchedInput{}
	// fetch saves one input and returns its ffmpeg input arguments: the demuxer is forced from the
	// sniffed bytes and only local files may be opened, so a playlist cannot make ffmpeg fetch anything.
	fetch := func(id string, video bool) ([]string, error) {
		input, ok := fetched[id]
		if !ok {
			input.name = fmt.Sprintf("in%d", len(fetched))
			if err := saveStorageObjectFile(id, filepath.Join(dir, input.name)); err != nil {
				log.Printf("compose input read failed: job=%s file=%s err=%v", job.ID, id, err)
				return nil, GenerationError{Message: "合成素材读取失败", Retryable: true}
			}
			input.format = sniffMediaFormat(filepath.Join(dir, input.name))
			fetched[id] = input
		}
		if input.format == "" || (video && !slices.Contains(composeVideoFormats, input.format)) {
			return nil, GenerationError{Message: "合成素材格式不支持"}
		}
		return []string{"-f", input.format, "-protocol_whitelist", "file", "-i", input.name}, nil
	}

	width, height := composeSize(request.Width, request.Height)
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	var filters, mix []string
	var concat, srt strings.Builder
	input, start := 0, 0.0
	for index, shot := range request.Shots {
		seconds := composeShotSeconds(shot.Seconds)
		in, err := fetch(shot.VideoFileID, true)
		if err != nil {
			return GenerationJobResult{}, err
		}
		args = append(args, in...)
		filters = append(filters, fmt.Sprintf("[%d:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p,tpad=stop_mode=clone:stop_duration=%g,trim=duration=%g,setpts=PTS-STARTPTS[v%d]",
			input, width, height, width, height, composeFPS, seconds, seconds, index))
		fmt.Fprintf(&concat, "[v%d]", index)
		input++
		if shot.AudioFileID != "" {
			in, err := fetch(shot.AudioFileID, false)
			if err != nil {
				return GenerationJobResult{}, err
			}
			args = append(args, in...)
			filters = append(filters, fmt.Sprintf("[%d:a]aformat=sample_rates=44100:channel_layouts=stereo,atrim=duration=%g,adelay=%d:all=1[a%d]", input, seconds, int(start*1000), index))
			mix = append(mix, fmt.Sprintf("[a%d]", index))
			input++
		}
		if text := srtText(shot.Subtitle); text != "" {
			fmt.Fprintf(&srt, "%d\n%s --> %s\n%s\n\n", index+1, srtTime(start), srtTime(start+seconds), text)
		}
		start += seconds
	}
	filters = append(filters, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[vcat]", concat.String(), len(request.Shots)))
	videoOut := "[vcat]"
	burned := false
	if request.Subtitles && srt.Len() > 0 {
		if err := os.WriteFile(filepath.Join(dir, "subs.srt"), []byte(srt.String()), 0o600); err != nil {
			return GenerationJobResult{}, GenerationError{Message: "视频合成失败", Retryable: true}
		}
		if font := cjkFontFamily(ctx); font != "" {
			// The relative path keeps the filter argument free of drive letters and backslashes.
			// libass sizes are relative to a 288-line canvas, so one size fits every resolution.
			filters = append(filters, fmt.Sprintf("[vcat]subtitles=subs.srt:force_style='FontName=%s,FontSize=16,Outline=1,MarginV=30'[vsub]", font))
			videoOut, burned = "[vsub]", true
		}
	}
	if request.BgmFileID != "" {
		in, err := fetch(request.BgmFileID, false)
		if err != nil {
			return GenerationJobResult{}, err
		}
		args = append(append(args, "-stream_loop", "-1"), in...)
		filters = append(filters, fmt.Sprintf("[%d:a]aformat=sample_rates=44100:channel_layouts=stereo,volume=%g,atrim=duration=%g[bgm]", input, composeBgmVolume, start))
		mix = append(mix, "[bgm]")
	}
	if len(mix) > 0 {
		// apad must be finite: an endless pad makes ffmpeg hang now and then under load.
		filters = append(filters, fmt.Sprintf("%samix=inputs=%d:duration=longest:normalize=0,apad=whole_dur=%g[aout]", strings.Join(mix, ""), len(mix), start))
	}
	args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", videoOut)
	if len(mix) > 0 {
		args = append(args, "-map", "[aout]", "-c:a", "aac", "-b:a", "128k")
	}
	args = append(args, "-threads", "2", "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-r", fmt.Sprint(composeFPS),
		"-t", fmt.Sprintf("%g", start), "-movflags", "+faststart", "out.mp4")
	// CommandContext kills ffmpeg when the timeout (or a cancel) fires.
	command := exec.CommandContext(ctx, ffmpeg, args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		log.Printf("compose ffmpeg failed: job=%s err=%v output=%s", job.ID, err, output)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return GenerationJobResult{}, GenerationError{Message: "视频合成超时"}
		}
		return GenerationJobResult{}, GenerationError{Message: "视频合成失败：" + lastLine(string(output))}
	}
	data, err := os.ReadFile(filepath.Join(dir, "out.mp4"))
	if err != nil || len(data) == 0 || len(data) > GenerationMaxMediaBytes {
		return GenerationJobResult{}, GenerationError{Message: "视频合成失败：输出文件无效"}
	}
	userCtx := WithUser(ctx, user)
	uploaded, err := UploadStorageObject(userCtx, "video.mp4", "video/mp4", data)
	if err != nil {
		log.Printf("compose upload failed: job=%s err=%v", job.ID, err)
		return GenerationJobResult{}, GenerationError{Message: "成片保存失败", Retryable: true}
	}
	result := GenerationJobResult{OutputFileID: uploaded.ID, OutputURL: uploaded.URL, Width: width, Height: height}
	if request.Subtitles && srt.Len() > 0 {
		subtitle, err := UploadStorageObject(userCtx, "subtitles.srt", "application/x-subrip", []byte(srt.String()))
		if err != nil {
			log.Printf("compose subtitle upload failed: job=%s err=%v", job.ID, err)
		} else {
			raw, _ := json.Marshal(ComposeOutput{SubtitleFileID: subtitle.ID, SubtitleURL: subtitle.URL, SubtitlesBurned: burned})
			result.OutputText = string(raw)
		}
	}
	return result, nil
}

// checkComposeRequest bounds a compose job and makes sure every file it reads is one of the job's
// inputs, which prepareGenerationJob has already checked belong to the user.
func checkComposeRequest(raw []byte, inputs []string) error {
	var request ComposeRequest
	if json.Unmarshal(raw, &request) != nil || request.Width <= 0 || request.Height <= 0 || len(request.Shots) == 0 || len(request.Shots) > composeMaxShots {
		return safeMessageError{message: fmt.Sprintf("合成参数无效（镜头 1-%d 个）", composeMaxShots)}
	}
	total := 0.0
	files := []string{request.BgmFileID}
	for _, shot := range request.Shots {
		if shot.VideoFileID == "" {
			return safeMessageError{message: "合成镜头缺少视频"}
		}
		total += composeShotSeconds(shot.Seconds)
		files = append(files, shot.VideoFileID, shot.AudioFileID)
	}
	if total > composeMaxSeconds {
		return safeMessageError{message: fmt.Sprintf("成片时长不能超过 %d 秒", composeMaxSeconds)}
	}
	for _, id := range files {
		if id != "" && !slices.Contains(inputs, id) {
			return safeMessageError{message: "合成素材无效"}
		}
	}
	return nil
}

func composeShotSeconds(seconds float64) float64 {
	return min(max(seconds, 0.5), 60)
}

// composeSize scales the requested size so the long side is at most 1920, keeps each side at least
// 256 and rounds both down to even numbers (libx264 needs them).
func composeSize(width, height int) (int, int) {
	long := max(width, height, composeMaxSide)
	side := func(value int) int {
		return min(max(value*composeMaxSide/long, composeMinSide), composeMaxSide) / 2 * 2
	}
	return side(width), side(height)
}

// composeVideoFormats are the demuxers a shot clip may use; audio may be any sniffMediaFormat result.
var composeVideoFormats = []string{"mov", "matroska"}

// sniffMediaFormat names the ffmpeg demuxer for a file's magic bytes, or "" for anything else
// (playlists, scripts, images): http.DetectContentType misses most audio formats.
func sniffMediaFormat(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	head := make([]byte, 12)
	if _, err := io.ReadFull(file, head); err != nil {
		return ""
	}
	switch {
	case string(head[4:8]) == "ftyp":
		return "mov"
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "matroska"
	case string(head[:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		return "wav"
	case string(head[:4]) == "OggS":
		return "ogg"
	case string(head[:4]) == "fLaC":
		return "flac"
	case string(head[:3]) == "ID3":
		return "mp3"
	case head[0] == 0xFF && head[1]&0xF6 == 0xF0:
		return "aac"
	case head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "mp3"
	}
	return ""
}

// srtText keeps a subtitle on one line and drops braces, which libass would read as override tags.
func srtText(text string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("{", "", "}", "").Replace(text)), " ")
}

func saveStorageObjectFile(id string, target string) error {
	download, err := DownloadStorageObject(id, "")
	if err != nil {
		return err
	}
	defer download.Stream.Close()
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	written, err := io.Copy(file, io.LimitReader(download.Stream, GenerationMaxMediaBytes+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written > GenerationMaxMediaBytes {
		err = fmt.Errorf("file exceeds %d bytes", GenerationMaxMediaBytes)
	}
	return err
}

// cjkFontFamily returns an installed font family that covers Chinese, or "" (subtitles then stay in the .srt).
func cjkFontFamily(ctx context.Context) string {
	output, err := exec.CommandContext(ctx, "fc-list", ":lang=zh", "family").Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	family, _, _ := strings.Cut(line, ",")
	return strings.NewReplacer("'", "", ":", "", ",", "").Replace(strings.TrimSpace(family))
}

func srtTime(seconds float64) string {
	duration := time.Duration(seconds * float64(time.Second)).Round(time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", int(duration.Hours()), int(duration.Minutes())%60, int(duration.Seconds())%60, duration.Milliseconds()%1000)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return truncateGenerationError(strings.TrimSpace(lines[len(lines)-1]))
}
