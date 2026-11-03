package service

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// Feature keys of private.featureModels. videoScript falls back to the vision list and keyframe to the image list.
const (
	FeatureVision      = "vision"
	FeatureImage       = "image"
	FeatureVideoScript = "videoScript"
	FeatureKeyframe    = "keyframe"
	FeatureVideo       = "video"
	FeatureTTS         = "tts"
	featureModelMax    = 5
)

var (
	featureModelKeys    = []string{FeatureVision, FeatureImage, FeatureVideoScript, FeatureKeyframe, FeatureVideo, FeatureTTS}
	featureModelParents = map[string]string{FeatureVideoScript: FeatureVision, FeatureKeyframe: FeatureImage}
	featureModelLabels  = map[string]string{FeatureVision: "识图", FeatureImage: "生图", FeatureVideoScript: "分镜脚本", FeatureKeyframe: "关键帧", FeatureVideo: "视频", FeatureTTS: "配音"}
)

// normalizeFeatureModels trims, dedupes and caps each list at five. Settings saved before feature models
// (no featureModels key) are seeded from the old single-model public fields, so reading them is idempotent.
func normalizeFeatureModels(lists map[string][]model.FeatureModel, public model.PublicSetting) map[string][]model.FeatureModel {
	if lists == nil {
		lists = map[string][]model.FeatureModel{}
		for key, name := range map[string]string{
			FeatureVision: public.Ecommerce.VisionModel, FeatureImage: public.Ecommerce.ImageModel, FeatureVideoScript: public.Video.ScriptModel,
			FeatureKeyframe: public.Video.KeyframeModel, FeatureVideo: public.Video.Model, FeatureTTS: public.Video.TTSModel,
		} {
			lists[key] = []model.FeatureModel{{Model: name}}
		}
	}
	out := make(map[string][]model.FeatureModel, len(featureModelKeys))
	for _, key := range featureModelKeys {
		items := []model.FeatureModel{}
		for _, item := range lists[key] {
			item.Group, item.Model = strings.TrimSpace(item.Group), strings.TrimSpace(item.Model)
			if item.Model != "" && len(items) < featureModelMax && !slices.Contains(items, item) {
				items = append(items, item)
			}
		}
		out[key] = items
	}
	return out
}

// applyFeatureModels puts the first model of each list into the public fields (model names only, never
// groups or channels), so price estimates keep working; normalizePublicSetting then fills the fallbacks.
func applyFeatureModels(public *model.PublicSetting, lists map[string][]model.FeatureModel) {
	first := func(key string) string {
		if len(lists[key]) == 0 {
			return ""
		}
		return lists[key][0].Model
	}
	public.Ecommerce.VisionModel, public.Ecommerce.ImageModel = first(FeatureVision), first(FeatureImage)
	video := &public.Video
	video.ScriptModel, video.KeyframeModel, video.Model, video.TTSModel = first(FeatureVideoScript), first(FeatureKeyframe), first(FeatureVideo), first(FeatureTTS)
	public.FeatureModelNames = make(map[string][]string, len(featureModelKeys))
	for _, key := range featureModelKeys {
		list := lists[key]
		if len(list) == 0 {
			list = lists[featureModelParents[key]]
		}
		names := []string{}
		for _, item := range list {
			if !slices.Contains(names, item.Model) {
				names = append(names, item.Model)
			}
		}
		public.FeatureModelNames[key] = names
	}
}

// featureCandidates is the ordered models feature runs with for user: its own list, else the list it falls
// back to, else the fallback model of its public field (the default model).
func featureCandidates(user model.AuthUser, feature string) ([]model.GenerationCandidate, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return nil, err
	}
	settings = normalizeSettings(settings)
	list := settings.Private.FeatureModels[feature]
	if len(list) == 0 {
		list = settings.Private.FeatureModels[featureModelParents[feature]]
	}
	public := settings.Public
	fallback := map[string]string{
		FeatureVision: public.Ecommerce.VisionModel, FeatureImage: public.Ecommerce.ImageModel, FeatureVideoScript: public.Video.ScriptModel,
		FeatureKeyframe: public.Video.KeyframeModel, FeatureVideo: public.Video.Model, FeatureTTS: public.Video.TTSModel,
	}[feature]
	if len(list) == 0 && fallback != "" {
		list = []model.FeatureModel{{Model: fallback}}
	}
	if len(list) == 0 {
		return nil, safeMessageError{message: "管理员尚未配置" + featureModelLabels[feature] + "模型"}
	}
	candidates := make([]model.GenerationCandidate, len(list))
	for i, item := range list {
		candidates[i] = model.GenerationCandidate{Model: item.Model, ChannelID: FeatureChannelID(user, item.Group)}
	}
	return candidates, nil
}

// FeatureChannelID is the channel a feature entry calls through: with SSO, the user's account group ("newapi@",
// which a team space turns into the team key) or "newapi@<group>"; users not linked to new-api (local admins)
// keep the admin channel pick. Without SSO the group is an admin channel id.
func FeatureChannelID(user model.AuthUser, group string) string {
	if !NewapiSSOEnabled() {
		return group
	}
	if base := ecommerceChannelID(user.ID); base != "" {
		return base + group
	}
	return ""
}

// IsFeatureModelKey reports whether feature is a key of private.featureModels.
func IsFeatureModelKey(feature string) bool {
	return slices.Contains(featureModelKeys, feature)
}

// NextGenerationCandidate switches job to its next fallback model.
func NextGenerationCandidate(job model.GenerationJob) (model.GenerationJob, error) {
	var request map[string]any
	if err := json.Unmarshal([]byte(job.RequestJSON), &request); err != nil {
		return job, err
	}
	next := job.Candidates[0]
	request["model"] = next.Model
	raw, err := json.Marshal(request)
	if err != nil {
		return job, err
	}
	job.Model, job.ChannelID, job.RequestJSON, job.Candidates = next.Model, next.ChannelID, string(raw), job.Candidates[1:]
	return job, nil
}
