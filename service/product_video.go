package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	productVideoMaxProducts = 6
	productVideoMaxShots    = 20
	productVideoMinShotSecs = 2
)

var (
	errProductVideoChanged = safeMessageError{message: "视频状态已更新，请刷新后重试"}
	errProductVideoBusy    = safeMessageError{message: "AI 任务进行中，请稍候"}
	productVideoDurations  = []int{15, 30, 45, 60}
	// productVideoStyles are the script directions of each style; "custom" uses the user's text.
	productVideoStyles = map[string]string{
		"unboxing": "开箱测评：第一人称拆箱，展示包装、外观细节与上手体验",
		"scene":    "场景种草：在真实生活场景中自然使用商品，突出氛围与使用感受",
		"demo":     "功能演示：逐个演示核心功能与使用效果，画面清晰直观",
		"promo":    "促销快剪：节奏快、镜头短，突出优惠力度与限时氛围",
		"custom":   "",
	}
	// productVideoRatios maps a ratio to its keyframe image size and its 720p video size.
	productVideoRatios = map[string]struct {
		Image         string
		Width, Height int
	}{
		"9:16": {"1024x1536", 720, 1280},
		"16:9": {"1536x1024", 1280, 720},
		"1:1":  {"1024x1024", 720, 720},
	}
	storyboardObjectPattern = regexp.MustCompile(`\{[^{}]*\}`)
)

type ProductVideoForm struct {
	ProductFileIDs []string `json:"productFileIds"`
	Name           string   `json:"name"`
	Intro          string   `json:"intro"`
	SellingPoints  string   `json:"sellingPoints"`
}

// ProductVideoSettings is step 2 of the wizard; an empty Voice means no voiceover, an empty
// BgmFileID no background music.
type ProductVideoSettings struct {
	Duration   int    `json:"duration"`
	Ratio      string `json:"ratio"`
	Resolution string `json:"resolution"`
	Style      string `json:"style"`
	StyleText  string `json:"styleText"`
	Voice      string `json:"voice"`
	BgmFileID  string `json:"bgmFileId"`
	Subtitles  bool   `json:"subtitles"`
}

// ProductVideoShot is one storyboard row and the jobs that make it: keyframe image → video, plus voiceover.
type ProductVideoShot struct {
	ID            string `json:"id"`
	Seconds       int    `json:"seconds"`
	Scene         string `json:"scene"`
	Camera        string `json:"camera"`
	Voiceover     string `json:"voiceover"`
	Subtitle      string `json:"subtitle"`
	KeyframeJobID string `json:"keyframeJobId"`
	VideoJobID    string `json:"videoJobId"`
	AudioJobID    string `json:"audioJobId"`
	Error         string `json:"error"`
}

// ProductVideo is the wizard state saved in generation_batches.context_json.
type ProductVideo struct {
	Form         ProductVideoForm     `json:"form"`
	Settings     ProductVideoSettings `json:"settings"`
	Shots        []ProductVideoShot   `json:"shots"`
	ScriptJobID  string               `json:"scriptJobId"`
	ComposeJobID string               `json:"composeJobId"`
	ShotSeq      int                  `json:"shotSeq"`
	Notice       string               `json:"notice"`
}

type ProductVideoInput struct {
	Form     ProductVideoForm     `json:"form"`
	Settings ProductVideoSettings `json:"settings"`
}

type ProductVideoView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	ProductVideo
	Jobs []model.GenerationJob `json:"jobs"`
}

type ProductVideoSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CoverURL     string    `json:"coverUrl"`
	ComposeJobID string    `json:"composeJobId"`
	CreatedAt    time.Time `json:"createdAt"`
}

// StoryboardShot is one shot as the script model writes it and as the user edits it.
type StoryboardShot struct {
	ID        string `json:"id,omitempty"`
	Seconds   int    `json:"seconds"`
	Scene     string `json:"scene"`
	Camera    string `json:"camera"`
	Voiceover string `json:"voiceover"`
	Subtitle  string `json:"subtitle"`
}

const productVideoScriptPrompt = `你是资深电商短视频编导。请为下面的商品写一条 {{duration}} 秒的带货短视频分镜脚本。
商品名称：{{name}}
商品介绍：{{intro}}
核心卖点：{{points}}
视频风格：{{style}}
画面比例：{{ratio}}
参考图：随附的图片是商品实物图。

要求：
1. 分镜 {{minShots}}-{{maxShots}} 个，每个镜头 {{minSeconds}}-{{maxSeconds}} 秒（整数），所有镜头时长之和必须等于 {{duration}} 秒。
2. 开场 3 秒内抓住注意力；中段展示卖点与使用场景；最后一个镜头给出购买引导。
3. scene 是写给视频生成模型的画面描述：主体、动作、场景、光线，具体可视化；商品外观必须与参考图一致；不要写字幕和文字。
4. camera 是镜头运动，如：缓慢推近、拉远、环绕、平移、固定机位。
5. voiceover 是该镜头的口播旁白，按每秒 4 个字估算，不超过「时长 × 4」个字；不需要旁白时为空字符串。
6. subtitle 是屏幕字幕，不超过 16 个字，可与旁白一致或提炼。
只输出 JSON，不要 Markdown 代码块，不要解释。格式：
{"shots":[{"seconds":3,"scene":"画面描述","camera":"镜头运动","voiceover":"旁白","subtitle":"字幕"}]}`

const productVideoKeyframePrompt = "请以商品图为参考，生成带货短视频第 {{index}} 个镜头的首帧画面：{{scene}}。画面比例 {{ratio}}；商品的外观、颜色、材质、结构和标识必须与商品图完全一致，商品清晰突出；写实摄影风格，光线自然；不要添加任何文字、字幕、水印或边框。"

const productVideoClipPrompt = "{{scene}}。镜头运动：{{camera}}。商品外观与首帧保持一致，动作自然流畅，画面稳定清晰，不要出现文字和水印。"

func productVideoSetting() (model.PublicVideoSetting, error) {
	settings, err := PublicSettings()
	if err != nil {
		return model.PublicVideoSetting{}, err
	}
	if settings.Video.Model == "" || settings.Video.ScriptModel == "" {
		return settings.Video, safeMessageError{message: "管理员尚未配置视频模型或脚本模型"}
	}
	return settings.Video, nil
}

func CreateProductVideo(ctx context.Context, user model.AuthUser, input ProductVideoInput) (ProductVideoView, error) {
	if err := normalizeProductVideoInput(&input, user.ID); err != nil {
		return ProductVideoView{}, err
	}
	video := ProductVideo{Form: input.Form, Settings: input.Settings, Shots: []ProductVideoShot{}}
	raw, err := json.Marshal(video)
	if err != nil {
		return ProductVideoView{}, err
	}
	batch := model.GenerationBatch{
		ID: newID("genbatch"), UserID: user.ID, Source: model.GenerationSourceProductVideo,
		Title: "带货视频 · " + firstNonEmpty(input.Form.Name, "未命名商品") + " · " + time.Now().Format("01-02 15:04"), ContextJSON: string(raw), Version: 1, CreatedAt: generationNow(),
	}
	if err := repository.CreateGenerationBatch(batch, nil); err != nil {
		return ProductVideoView{}, err
	}
	return productVideoView(batch, video, []model.GenerationJob{}), nil
}

// GetProductVideo returns the wizard after applying finished jobs: the script lands in the storyboard
// and each finished keyframe queues its shot's video.
func GetProductVideo(ctx context.Context, user model.AuthUser, id string) (ProductVideoView, error) {
	batch, video, jobs, err := loadProductVideo(user.ID, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	created, changed := advanceProductVideo(ctx, user, batch, &video, jobs)
	if !changed {
		return productVideoView(batch, video, jobs), nil
	}
	if err := commitProductVideo(batch, video, created); err != nil {
		if err != errProductVideoChanged {
			return ProductVideoView{}, err
		}
		// Another request advanced it first; show its result.
		if batch, video, jobs, err = loadProductVideo(user.ID, id); err != nil {
			return ProductVideoView{}, err
		}
		return productVideoView(batch, video, jobs), nil
	}
	return productVideoView(batch, video, append(jobs, created...)), nil
}

// UpdateProductVideo saves steps 1 and 2; the storyboard and generated shots are kept.
func UpdateProductVideo(ctx context.Context, user model.AuthUser, id string, input ProductVideoInput) (ProductVideoView, error) {
	batch, video, created, err := openProductVideo(ctx, user, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	if video.ScriptJobID != "" {
		return ProductVideoView{}, errProductVideoBusy
	}
	if err := normalizeProductVideoInput(&input, user.ID); err != nil {
		return ProductVideoView{}, err
	}
	video.Form, video.Settings = input.Form, input.Settings
	return commitAndGetProductVideo(ctx, user, batch, video, created)
}

// GenerateProductVideoScript queues the storyboard job (vision model over the product images).
func GenerateProductVideoScript(ctx context.Context, user model.AuthUser, id string) (ProductVideoView, error) {
	batch, video, created, err := openProductVideo(ctx, user, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	if video.ScriptJobID != "" {
		return ProductVideoView{}, errProductVideoBusy
	}
	setting, err := productVideoSetting()
	if err != nil {
		return ProductVideoView{}, err
	}
	instruction := buildStoryboardInstruction(video, setting.MaxShotSeconds)
	job, err := newEcommerceVisionJob(ctx, user, batch, setting.ScriptModel, instruction, video.Form.ProductFileIDs, "AI 生成分镜")
	if err != nil {
		return ProductVideoView{}, err
	}
	video.ScriptJobID, video.Notice = job.ID, ""
	return commitAndGetProductVideo(ctx, user, batch, video, append(created, job))
}

// SaveProductVideoScript replaces the storyboard with the user's edits. Shots keep their generated
// clips unless what made them changed: picture edits drop keyframe and video, voiceover edits the audio.
func SaveProductVideoScript(ctx context.Context, user model.AuthUser, id string, shots []StoryboardShot) (ProductVideoView, error) {
	batch, video, created, err := openProductVideo(ctx, user, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	if video.ScriptJobID != "" {
		return ProductVideoView{}, errProductVideoBusy
	}
	setting, err := productVideoSetting()
	if err != nil {
		return ProductVideoView{}, err
	}
	if len(shots) == 0 || len(shots) > productVideoMaxShots {
		return ProductVideoView{}, safeMessageError{message: fmt.Sprintf("分镜数量需为 1-%d 个", productVideoMaxShots)}
	}
	total := 0
	next := make([]ProductVideoShot, 0, len(shots))
	for index, edit := range shots {
		edit = trimStoryboardShot(edit)
		if edit.Seconds < productVideoMinShotSecs || edit.Seconds > setting.MaxShotSeconds {
			return ProductVideoView{}, safeMessageError{message: fmt.Sprintf("第 %d 镜时长需为 %d-%d 秒", index+1, productVideoMinShotSecs, setting.MaxShotSeconds)}
		}
		if edit.Scene == "" || len([]rune(edit.Scene)) > 500 || len([]rune(edit.Camera)) > 100 || len([]rune(edit.Voiceover)) > 300 || len([]rune(edit.Subtitle)) > 50 {
			return ProductVideoView{}, safeMessageError{message: fmt.Sprintf("第 %d 镜内容无效：画面描述必填（≤500 字），镜头 ≤100 字，旁白 ≤300 字，字幕 ≤50 字", index+1)}
		}
		total += edit.Seconds
		shot := ProductVideoShot{}
		if old := slices.IndexFunc(video.Shots, func(item ProductVideoShot) bool { return edit.ID != "" && item.ID == edit.ID }); old >= 0 {
			shot = video.Shots[old]
			if shot.Seconds != edit.Seconds || shot.Scene != edit.Scene || shot.Camera != edit.Camera {
				shot.KeyframeJobID, shot.VideoJobID, shot.Error = "", "", ""
			}
			if shot.Voiceover != edit.Voiceover {
				shot.AudioJobID = ""
			}
		} else {
			video.ShotSeq++
			shot.ID = fmt.Sprintf("shot_%02d", video.ShotSeq)
		}
		shot.Seconds, shot.Scene, shot.Camera, shot.Voiceover, shot.Subtitle = edit.Seconds, edit.Scene, edit.Camera, edit.Voiceover, edit.Subtitle
		next = append(next, shot)
	}
	if total > video.Settings.Duration {
		return ProductVideoView{}, safeMessageError{message: fmt.Sprintf("分镜总时长 %d 秒超过视频时长 %d 秒", total, video.Settings.Duration)}
	}
	video.Shots, video.ComposeJobID = next, ""
	return commitAndGetProductVideo(ctx, user, batch, video, created)
}

// GenerateProductVideoShots queues the keyframe (or, without a keyframe model, the video) and the
// voiceover of the given shots; an empty list means every shot without a finished or running clip.
func GenerateProductVideoShots(ctx context.Context, user model.AuthUser, id string, shotIDs []string) (ProductVideoView, error) {
	batch, video, created, err := openProductVideo(ctx, user, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	if video.ScriptJobID != "" {
		return ProductVideoView{}, errProductVideoBusy
	}
	if len(video.Shots) == 0 {
		return ProductVideoView{}, safeMessageError{message: "请先生成分镜脚本"}
	}
	setting, err := productVideoSetting()
	if err != nil {
		return ProductVideoView{}, err
	}
	jobs, err := repository.ListUserBatchGenerationJobs(user.ID, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	byID := generationJobsByID(append(jobs, created...))
	queued := 0
	for index := range video.Shots {
		shot := &video.Shots[index]
		if len(shotIDs) > 0 && !slices.Contains(shotIDs, shot.ID) {
			continue
		}
		if len(shotIDs) == 0 && shot.Error == "" && productVideoShotBusyOrDone(*shot, byID) {
			continue
		}
		next, err := queueProductVideoShot(ctx, user, batch, video, index, setting)
		if err != nil {
			return ProductVideoView{}, err
		}
		created = append(created, next...)
		queued++
	}
	if len(shotIDs) > 0 && queued == 0 {
		return ProductVideoView{}, safeMessageError{message: "分镜不存在"}
	}
	if queued == 0 {
		return ProductVideoView{}, safeMessageError{message: "所有分镜都已生成或正在生成"}
	}
	video.ComposeJobID = ""
	return commitAndGetProductVideo(ctx, user, batch, video, created)
}

// ComposeProductVideo queues the ffmpeg job that joins every finished shot into the final video.
func ComposeProductVideo(ctx context.Context, user model.AuthUser, id string) (ProductVideoView, error) {
	batch, video, created, err := openProductVideo(ctx, user, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	if len(video.Shots) == 0 {
		return ProductVideoView{}, safeMessageError{message: "请先生成分镜脚本"}
	}
	jobs, err := repository.ListUserBatchGenerationJobs(user.ID, id)
	if err != nil {
		return ProductVideoView{}, err
	}
	byID := generationJobsByID(jobs)
	if current, ok := byID[video.ComposeJobID]; ok && generationJobActive(current.Status) {
		return ProductVideoView{}, safeMessageError{message: "成片正在合成，请稍候"}
	}
	width, height := productVideoSize(video.Settings)
	request := ComposeRequest{Width: width, Height: height, BgmFileID: video.Settings.BgmFileID, Subtitles: video.Settings.Subtitles}
	inputs := []string{}
	for index, shot := range video.Shots {
		clip := byID[shot.VideoJobID]
		if clip.Status != model.GenerationStatusSucceeded || clip.OutputFileID == "" {
			return ProductVideoView{}, safeMessageError{message: fmt.Sprintf("第 %d 镜视频尚未生成完成", index+1)}
		}
		item := ComposeShot{VideoFileID: clip.OutputFileID, Seconds: float64(shot.Seconds), Subtitle: shot.Subtitle}
		if shot.AudioJobID != "" {
			audio := byID[shot.AudioJobID]
			if audio.Status != model.GenerationStatusSucceeded || audio.OutputFileID == "" {
				return ProductVideoView{}, safeMessageError{message: fmt.Sprintf("第 %d 镜配音尚未生成完成", index+1)}
			}
			item.AudioFileID = audio.OutputFileID
			inputs = append(inputs, audio.OutputFileID)
		}
		request.Shots = append(request.Shots, item)
		inputs = append(inputs, clip.OutputFileID)
	}
	if request.BgmFileID != "" {
		inputs = append(inputs, request.BgmFileID)
	}
	raw, _ := json.Marshal(request)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	next, err := buildGenerationJobs(ctx, user, batch, []GenerationJobInput{{
		Kind: model.GenerationKindCompose, Endpoint: "ffmpeg", Model: "ffmpeg", Request: fields,
		InputFileIDs: slices.Compact(slices.Sorted(slices.Values(inputs))), Prompt: batch.Title,
	}})
	if err != nil {
		return ProductVideoView{}, err
	}
	video.ComposeJobID = next[0].ID
	return commitAndGetProductVideo(ctx, user, batch, video, append(created, next...))
}

func ListProductVideos(userID string) ([]ProductVideoSummary, error) {
	batches, err := repository.ListUserGenerationBatches(userID, model.GenerationSourceProductVideo, 50)
	if err != nil {
		return nil, err
	}
	summaries := []ProductVideoSummary{}
	for _, batch := range batches {
		var video ProductVideo
		if json.Unmarshal([]byte(batch.ContextJSON), &video) != nil {
			continue
		}
		cover := ""
		if len(video.Form.ProductFileIDs) > 0 {
			cover = "/api/files/" + video.Form.ProductFileIDs[0] + "/content"
		}
		summaries = append(summaries, ProductVideoSummary{ID: batch.ID, Title: batch.Title, CoverURL: cover, ComposeJobID: video.ComposeJobID, CreatedAt: batch.CreatedAt})
	}
	return summaries, nil
}

// queueProductVideoShot builds the jobs of one shot (unsaved) and points the shot at them.
func queueProductVideoShot(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, video ProductVideo, index int, setting model.PublicVideoSetting) ([]model.GenerationJob, error) {
	shot := &video.Shots[index]
	ratio := productVideoRatios[video.Settings.Ratio]
	channelID := ecommerceChannelID(user.ID)
	var items []GenerationJobInput
	if setting.KeyframeModel != "" {
		prompt := renderPromptTemplate(productVideoKeyframePrompt, map[string]string{"index": strconv.Itoa(index + 1), "scene": shot.Scene, "ratio": video.Settings.Ratio})
		items = append(items, GenerationJobInput{
			Kind: model.GenerationKindImage, Endpoint: "/images/edits", Model: setting.KeyframeModel, ChannelID: channelID,
			Request: map[string]any{"prompt": prompt, "size": ratio.Image}, InputFileIDs: video.Form.ProductFileIDs, Prompt: prompt, SlotKind: "keyframe",
		})
	} else {
		items = append(items, productVideoClipInput(video, *shot, video.Form.ProductFileIDs[0], setting, channelID))
	}
	withAudio := video.Settings.Voice != "" && shot.Voiceover != "" && setting.TTSModel != ""
	if withAudio {
		items = append(items, GenerationJobInput{
			Kind: model.GenerationKindAudio, Endpoint: "/audio/speech", Model: setting.TTSModel, ChannelID: channelID,
			Request: map[string]any{"input": shot.Voiceover, "voice": video.Settings.Voice, "response_format": "mp3"}, Prompt: shot.Voiceover, SlotKind: "voiceover",
		})
	}
	jobs, err := buildGenerationJobs(ctx, user, batch, items)
	if err != nil {
		return nil, err
	}
	shot.KeyframeJobID, shot.VideoJobID, shot.AudioJobID, shot.Error = "", "", "", ""
	if setting.KeyframeModel != "" {
		shot.KeyframeJobID = jobs[0].ID
	} else {
		shot.VideoJobID = jobs[0].ID
	}
	if withAudio {
		shot.AudioJobID = jobs[1].ID
	}
	return jobs, nil
}

func productVideoClipInput(video ProductVideo, shot ProductVideoShot, frameFileID string, setting model.PublicVideoSetting, channelID string) GenerationJobInput {
	width, height := productVideoSize(video.Settings)
	prompt := renderPromptTemplate(productVideoClipPrompt, map[string]string{"scene": shot.Scene, "camera": firstNonEmpty(shot.Camera, "固定机位")})
	return GenerationJobInput{
		Kind: model.GenerationKindVideo, Endpoint: "/videos", Model: setting.Model, ChannelID: channelID,
		Request:      map[string]any{"prompt": prompt, "seconds": strconv.Itoa(shot.Seconds), "size": fmt.Sprintf("%dx%d", width, height)},
		InputFileIDs: []string{frameFileID}, Prompt: prompt, SlotKind: "clip",
	}
}

// advanceProductVideo applies finished jobs; video jobs it builds are returned unsaved so the caller
// persists them only after winning the version check.
func advanceProductVideo(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, video *ProductVideo, jobs []model.GenerationJob) ([]model.GenerationJob, bool) {
	byID := generationJobsByID(jobs)
	changed := false
	if job, found := byID[video.ScriptJobID]; video.ScriptJobID != "" && (!found || !generationJobActive(job.Status)) {
		changed = true
		video.ScriptJobID = ""
		shots := parseStoryboard(job.OutputText)
		switch {
		case job.Status != model.GenerationStatusSucceeded:
			video.Notice = "分镜生成失败：" + firstNonEmpty(job.Error, "任务未完成")
		case len(shots) == 0:
			video.Notice = "分镜结果无法识别，请重试"
		default:
			video.Shots, video.ComposeJobID, video.Notice = []ProductVideoShot{}, "", ""
			setting, _ := productVideoSetting()
			total := 0
			for _, item := range shots {
				seconds := min(max(item.Seconds, productVideoMinShotSecs), max(setting.MaxShotSeconds, productVideoMinShotSecs))
				if len(video.Shots) == productVideoMaxShots || total+seconds > video.Settings.Duration {
					break
				}
				total += seconds
				video.ShotSeq++
				video.Shots = append(video.Shots, ProductVideoShot{ID: fmt.Sprintf("shot_%02d", video.ShotSeq), Seconds: seconds, Scene: item.Scene, Camera: item.Camera, Voiceover: item.Voiceover, Subtitle: item.Subtitle})
			}
		}
	}
	var created []model.GenerationJob
	for index := range video.Shots {
		shot := &video.Shots[index]
		keyframe, found := byID[shot.KeyframeJobID]
		if !found || shot.VideoJobID != "" || shot.Error != "" || keyframe.Status != model.GenerationStatusSucceeded {
			continue
		}
		changed = true
		setting, err := productVideoSetting()
		var next []model.GenerationJob
		if err == nil {
			next, err = buildGenerationJobs(ctx, user, batch, []GenerationJobInput{productVideoClipInput(*video, *shot, keyframe.OutputFileID, setting, ecommerceChannelID(user.ID))})
		}
		if err != nil {
			log.Printf("queue product video clip failed: batch=%s shot=%s err=%v", batch.ID, shot.ID, err)
			shot.Error = "视频任务提交失败：" + err.Error()
			continue
		}
		shot.VideoJobID = next[0].ID
		created = append(created, next...)
	}
	return created, changed
}

func openProductVideo(ctx context.Context, user model.AuthUser, id string) (model.GenerationBatch, ProductVideo, []model.GenerationJob, error) {
	batch, video, jobs, err := loadProductVideo(user.ID, id)
	if err != nil {
		return model.GenerationBatch{}, ProductVideo{}, nil, err
	}
	created, _ := advanceProductVideo(ctx, user, batch, &video, jobs)
	return batch, video, created, nil
}

func loadProductVideo(userID string, id string) (model.GenerationBatch, ProductVideo, []model.GenerationJob, error) {
	batch, ok, err := repository.GetUserGenerationBatch(userID, id)
	if err != nil {
		return model.GenerationBatch{}, ProductVideo{}, nil, err
	}
	if !ok || batch.Source != model.GenerationSourceProductVideo {
		return model.GenerationBatch{}, ProductVideo{}, nil, safeMessageError{message: "带货视频不存在"}
	}
	var video ProductVideo
	if err := json.Unmarshal([]byte(batch.ContextJSON), &video); err != nil {
		return model.GenerationBatch{}, ProductVideo{}, nil, err
	}
	if video.Shots == nil {
		video.Shots = []ProductVideoShot{}
	}
	jobs, err := repository.ListUserBatchGenerationJobs(userID, id)
	return batch, video, jobs, err
}

func commitAndGetProductVideo(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, video ProductVideo, jobs []model.GenerationJob) (ProductVideoView, error) {
	if err := commitProductVideo(batch, video, jobs); err != nil {
		return ProductVideoView{}, err
	}
	return GetProductVideo(ctx, user, batch.ID)
}

// commitProductVideo saves the state if no other request saved since it was read, then queues jobs.
func commitProductVideo(batch model.GenerationBatch, video ProductVideo, jobs []model.GenerationJob) error {
	raw, err := json.Marshal(video)
	if err != nil {
		return err
	}
	ok, err := repository.UpdateGenerationBatchContext(batch.UserID, batch.ID, batch.Version, string(raw))
	if err != nil {
		return err
	}
	if !ok {
		return errProductVideoChanged
	}
	if err := repository.CreateGenerationJobs(jobs); err != nil {
		return err
	}
	if len(jobs) > 0 {
		WakeGenerationWorker()
	}
	return nil
}

func productVideoView(batch model.GenerationBatch, video ProductVideo, jobs []model.GenerationJob) ProductVideoView {
	return ProductVideoView{ID: batch.ID, Title: batch.Title, CreatedAt: batch.CreatedAt, ProductVideo: video, Jobs: jobs}
}

func normalizeProductVideoInput(input *ProductVideoInput, userID string) error {
	form, settings := &input.Form, &input.Settings
	if len(form.ProductFileIDs) < 1 || len(form.ProductFileIDs) > productVideoMaxProducts {
		return safeMessageError{message: fmt.Sprintf("请上传 1-%d 张产品图", productVideoMaxProducts)}
	}
	for _, id := range form.ProductFileIDs {
		if err := checkEcommerceImage(userID, id, false); err != nil {
			return err
		}
	}
	form.Name, form.Intro, form.SellingPoints = strings.TrimSpace(form.Name), strings.TrimSpace(form.Intro), strings.TrimSpace(form.SellingPoints)
	if form.Name == "" || len([]rune(form.Name)) > 50 || len([]rune(form.Intro)) > 1000 || len([]rune(form.SellingPoints)) > 1000 {
		return safeMessageError{message: "产品名称必填（≤50 字），产品介绍与卖点各 ≤1000 字"}
	}
	if settings.Duration == 0 {
		settings.Duration = 30
	}
	if !slices.Contains(productVideoDurations, settings.Duration) {
		return safeMessageError{message: "视频时长需为 15 / 30 / 45 / 60 秒"}
	}
	settings.Ratio = firstNonEmpty(settings.Ratio, "9:16")
	if _, ok := productVideoRatios[settings.Ratio]; !ok {
		return safeMessageError{message: "视频比例无效"}
	}
	settings.Resolution = firstNonEmpty(settings.Resolution, "720p")
	if settings.Resolution != "720p" && settings.Resolution != "1080p" {
		return safeMessageError{message: "分辨率无效"}
	}
	settings.Style = firstNonEmpty(settings.Style, "scene")
	settings.StyleText = strings.TrimSpace(settings.StyleText)
	if _, ok := productVideoStyles[settings.Style]; !ok || len([]rune(settings.StyleText)) > 300 || (settings.Style == "custom" && settings.StyleText == "") {
		return safeMessageError{message: "视频风格无效（自定义风格需填写 ≤300 字说明）"}
	}
	settings.Voice = strings.TrimSpace(settings.Voice)
	if len(settings.Voice) > 64 {
		return safeMessageError{message: "音色无效"}
	}
	if settings.BgmFileID != "" {
		object, err := repository.GetStorageObject(settings.BgmFileID)
		if err != nil || object.CreatedBy != userID || !strings.HasPrefix(object.MimeType, "audio/") {
			return safeMessageError{message: "背景音乐需为自己上传的音频文件"}
		}
	}
	return nil
}

func productVideoSize(settings ProductVideoSettings) (int, int) {
	ratio := productVideoRatios[settings.Ratio]
	if settings.Resolution == "1080p" {
		return ratio.Width * 3 / 2, ratio.Height * 3 / 2
	}
	return ratio.Width, ratio.Height
}

func buildStoryboardInstruction(video ProductVideo, maxShotSeconds int) string {
	style := productVideoStyles[video.Settings.Style]
	if video.Settings.StyleText != "" {
		style = strings.TrimSpace(style + "；" + video.Settings.StyleText)
	}
	maxShotSeconds = max(maxShotSeconds, productVideoMinShotSecs)
	minShots := (video.Settings.Duration + maxShotSeconds - 1) / maxShotSeconds
	return renderPromptTemplate(productVideoScriptPrompt, map[string]string{
		"duration": strconv.Itoa(video.Settings.Duration), "name": video.Form.Name, "intro": firstNonEmpty(video.Form.Intro, "（无）"),
		"points": firstNonEmpty(video.Form.SellingPoints, "（无，请根据商品图提炼）"), "style": firstNonEmpty(style, "场景种草"), "ratio": video.Settings.Ratio,
		"minShots": strconv.Itoa(minShots), "maxShots": strconv.Itoa(min(max(minShots, video.Settings.Duration/3), productVideoMaxShots)),
		"minSeconds": strconv.Itoa(productVideoMinShotSecs), "maxSeconds": strconv.Itoa(maxShotSeconds),
	})
}

// parseStoryboard reads the script reply: the whole {"shots":[…]} object when it parses, otherwise
// every complete shot object of a truncated or chatty reply.
func parseStoryboard(text string) []StoryboardShot {
	var reply struct {
		Shots []StoryboardShot `json:"shots"`
	}
	candidate := text
	if match := fencedJSONPattern.FindStringSubmatch(text); match != nil {
		candidate = match[1]
	}
	if start, end := strings.Index(candidate, "{"), strings.LastIndex(candidate, "}"); start >= 0 && end > start {
		_ = json.Unmarshal([]byte(candidate[start:end+1]), &reply)
	}
	if len(reply.Shots) == 0 {
		for _, object := range storyboardObjectPattern.FindAllString(text, -1) {
			var shot StoryboardShot
			if json.Unmarshal([]byte(object), &shot) == nil {
				reply.Shots = append(reply.Shots, shot)
			}
		}
	}
	shots := []StoryboardShot{}
	for _, shot := range reply.Shots {
		if shot = trimStoryboardShot(shot); shot.Scene != "" {
			shots = append(shots, shot)
		}
	}
	return shots
}

func trimStoryboardShot(shot StoryboardShot) StoryboardShot {
	cut := func(text string, limit int) string {
		if runes := []rune(strings.TrimSpace(text)); len(runes) > limit {
			return string(runes[:limit])
		}
		return strings.TrimSpace(text)
	}
	shot.ID = strings.TrimSpace(shot.ID)
	shot.Scene, shot.Camera, shot.Voiceover, shot.Subtitle = strings.TrimSpace(shot.Scene), cut(shot.Camera, 100), strings.TrimSpace(shot.Voiceover), strings.TrimSpace(shot.Subtitle)
	return shot
}

// productVideoShotBusyOrDone reports whether a shot already has a clip finished or on its way.
func productVideoShotBusyOrDone(shot ProductVideoShot, jobs map[string]model.GenerationJob) bool {
	for _, id := range []string{shot.VideoJobID, shot.KeyframeJobID} {
		if job, ok := jobs[id]; ok {
			return job.Status == model.GenerationStatusSucceeded || generationJobActive(job.Status)
		}
	}
	return false
}

func generationJobsByID(jobs []model.GenerationJob) map[string]model.GenerationJob {
	byID := make(map[string]model.GenerationJob, len(jobs))
	for _, job := range jobs {
		byID[job.ID] = job
	}
	return byID
}

func generationJobActive(status string) bool {
	return slices.Contains(repository.ActiveGenerationStatuses, status)
}
