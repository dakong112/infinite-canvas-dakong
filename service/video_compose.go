package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
)

const (
	// GenerationMaxMediaBytes caps one stored video or audio result (and one compose input).
	GenerationMaxMediaBytes = 200 << 20
	composeFPS              = 30
	composeBgmVolume        = 0.15
)

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
	dir, err := os.MkdirTemp("", "compose-")
	if err != nil {
		return GenerationJobResult{}, GenerationError{Message: "视频合成失败", Retryable: true}
	}
	defer os.RemoveAll(dir)
	fetched := map[string]string{}
	fetch := func(id string) (string, error) {
		if name, ok := fetched[id]; ok {
			return name, nil
		}
		name := fmt.Sprintf("in%d", len(fetched))
		if err := saveStorageObjectFile(id, filepath.Join(dir, name)); err != nil {
			log.Printf("compose input read failed: job=%s file=%s err=%v", job.ID, id, err)
			return "", GenerationError{Message: "合成素材读取失败", Retryable: true}
		}
		fetched[id] = name
		return name, nil
	}

	width, height := request.Width/2*2, request.Height/2*2
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	var filters, mix []string
	var concat, srt strings.Builder
	input, start := 0, 0.0
	for index, shot := range request.Shots {
		seconds := min(max(shot.Seconds, 0.5), 60)
		name, err := fetch(shot.VideoFileID)
		if err != nil {
			return GenerationJobResult{}, err
		}
		args = append(args, "-i", name)
		filters = append(filters, fmt.Sprintf("[%d:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p,tpad=stop_mode=clone:stop_duration=%g,trim=duration=%g,setpts=PTS-STARTPTS[v%d]",
			input, width, height, width, height, composeFPS, seconds, seconds, index))
		fmt.Fprintf(&concat, "[v%d]", index)
		input++
		if shot.AudioFileID != "" {
			name, err := fetch(shot.AudioFileID)
			if err != nil {
				return GenerationJobResult{}, err
			}
			args = append(args, "-i", name)
			filters = append(filters, fmt.Sprintf("[%d:a]aformat=sample_rates=44100:channel_layouts=stereo,atrim=duration=%g,adelay=%d:all=1[a%d]", input, seconds, int(start*1000), index))
			mix = append(mix, fmt.Sprintf("[a%d]", index))
			input++
		}
		if text := strings.TrimSpace(shot.Subtitle); text != "" {
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
		name, err := fetch(request.BgmFileID)
		if err != nil {
			return GenerationJobResult{}, err
		}
		args = append(args, "-stream_loop", "-1", "-i", name)
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
	args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-r", fmt.Sprint(composeFPS),
		"-t", fmt.Sprintf("%g", start), "-movflags", "+faststart", "out.mp4")
	command := exec.CommandContext(ctx, ffmpeg, args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		log.Printf("compose ffmpeg failed: job=%s err=%v output=%s", job.ID, err, output)
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
