package service

import (
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

var allResolutions = []string{"1K", "2K", "4K"}

// defaultImageTemplates are our own starter templates, seeded as drafts for the admin to review,
// add covers and examples to, and publish.
var defaultImageTemplates = []model.ImageTemplate{
	{
		Slug: "outfit-scenes", Category: "ecommerce", Title: "多场景穿搭套图",
		Description: "上传服饰，一次得到通勤、街拍、居家等不同场景的上身效果图",
		Inputs:      []model.ImageTemplateInput{{Key: "garment", Label: "服饰图", Max: 3, Required: true}, {Key: "model", Label: "模特图", Max: 1}},
		Prompt: "请把「服饰图」中的服饰完整穿搭到一位模特身上，生成一张真实自然的电商穿搭展示图。若提供了「模特图」，保持该模特的五官、发型与身材不变；否则根据服饰风格选择气质相符的模特。" +
			"服饰的款式、颜色、图案、面料质感与版型必须与服饰图一致，多件服饰按上下装、外套、配饰合理组合。场景从城市街头、通勤办公、咖啡馆、居家、户外度假中选择一个与服饰最搭的，光线自然，构图突出穿搭整体。" +
			"人物手脚与肢体比例正常，不要出现文字和水印。{{prompt}}",
		PromptPlaceholder: "可选：指定场景或模特气质，如“海边度假，阳光感”",
		Ratios:            []string{"3:4", "2:3", "1:1", "9:16"},
	},
	{
		Slug: "product-scene", Category: "ecommerce", Title: "商品场景展示",
		Description: "把商品放进贴合调性的真实使用场景，做出有氛围感的主图",
		Inputs:      []model.ImageTemplateInput{{Key: "product", Label: "商品图", Max: 2, Required: true}},
		Prompt: "以 @图片1 中的商品为唯一主角，为它搭建一个真实可信的使用场景并拍成电商场景图。商品的外形、比例、颜色、材质与标识必须如实保留，不得变形或替换。" +
			"场景、道具与光影要贴合商品的定位和目标人群，商品位于视觉中心、清晰锐利，背景适度虚化，整体干净高级，不要添加任何文字。未指定场景时自行选择最能衬托商品的场景。{{prompt}}",
		PromptPlaceholder: "描述想要的场景，如“清晨洒满阳光的木质餐桌”",
		Ratios:            []string{"1:1", "3:4", "4:3", "16:9"},
	},
	{
		Slug: "white-background", Category: "ecommerce", Title: "白底图提取",
		Description: "一键得到符合平台规范的纯白底商品图",
		Inputs:      []model.ImageTemplateInput{{Key: "product", Label: "商品图", Max: 1, Required: true}},
		Prompt: "把 @图片1 中的商品单独提取出来，放在纯白色 #FFFFFF 背景正中，生成符合电商平台规范的白底图。商品完整无裁切，占画面约七到八成，边缘干净利落、无残留背景与毛边；" +
			"保留商品原有的形状、颜色、纹理和细节，只允许商品下方有极淡的自然投影；画面中不要出现其他物体、文字或水印。{{prompt}}",
		PromptPlaceholder: "可选：补充说明，如“去掉吊牌”",
		Ratios:            []string{"1:1", "3:4"},
	},
	{
		Slug: "model-wear", Category: "ecommerce", Title: "模特试戴",
		Description: "上传首饰、眼镜、帽子等配饰，生成真人佩戴的近景展示图",
		Inputs:      []model.ImageTemplateInput{{Key: "accessory", Label: "配饰图", Max: 2, Required: true}, {Key: "model", Label: "模特图", Max: 1}},
		Prompt: "让一位模特自然地佩戴「配饰图」中的配饰，生成近景电商佩戴展示图。若提供了「模特图」，保持该模特的面部特征与发型；否则选择与配饰风格相符的模特。" +
			"配饰的造型、材质、颜色和光泽必须与原图一致，佩戴位置和尺寸比例真实合理，以浅景深突出配饰细节，皮肤质感自然，不要出现文字。{{prompt}}",
		PromptPlaceholder: "可选：如“法式复古妆容，侧脸角度”",
		Ratios:            []string{"3:4", "1:1", "4:5"},
	},
	{
		Slug: "multi-angle", Category: "ecommerce", Title: "商品多角度视图",
		Description: "根据商品图生成正面、侧面、背面、俯视等多角度展示",
		Inputs:      []model.ImageTemplateInput{{Key: "product", Label: "商品图", Max: 2, Required: true}},
		Prompt: "参考上传的商品图，生成同一件商品的多角度展示拼图：在同一画面中整齐排列正面、45 度侧面、正侧面、背面和俯视五个视角，每个视角大小一致、间距均匀。" +
			"所有视角中的商品造型、颜色、材质与细节保持完全一致，看不到的部分按商品结构合理推断；统一使用浅灰色无缝背景和柔和棚拍光，不要添加文字标注。{{prompt}}",
		PromptPlaceholder: "可选：指定需要的角度或背景色",
		Ratios:            []string{"16:9", "4:3", "1:1"},
	},
	{
		Slug: "text-poster", Category: "ecommerce", Title: "爆款文字海报",
		Description: "输入标题和卖点，生成带醒目大字的促销海报",
		Inputs:      []model.ImageTemplateInput{{Key: "product", Label: "商品图", Max: 1, Required: true}},
		Prompt: "以 @图片1 中的商品为主体，设计一张吸睛的电商促销海报。商品外观如实还原并放在视觉焦点位置；画面中排版以下文案：{{prompt}}。" +
			"主标题字号最大、粗体醒目，卖点用短句或标签呈现，文字必须准确无错别字、清晰可读；配色与商品主色协调，层次分明，留出呼吸感，整体有冲击力但不杂乱。",
		PromptRequired:    true,
		PromptPlaceholder: "输入主标题与卖点，如“夏日清凉 第二件半价 | 冰感面料 速干透气”",
		Ratios:            []string{"3:4", "9:16", "1:1"},
	},
	{
		Slug: "storyboard", Category: "creative", Title: "分镜图生成",
		Description: "用一句剧情描述生成连贯的多格分镜，可上传角色参考保持一致",
		Inputs:      []model.ImageTemplateInput{{Key: "reference", Label: "角色或场景参考", Max: 2}},
		Prompt: "根据下面的剧情生成一张六格电影分镜图，按从左到右、从上到下的顺序排列，每格下方用一行简短中文标注镜头编号与景别。" +
			"六格依次推进剧情，镜头语言富于变化（远景、中景、特写、过肩等），人物形象、服装和画风在各格中保持一致；若提供了「角色或场景参考」，以其为人物和场景设定。剧情：{{prompt}}",
		PromptRequired:    true,
		PromptPlaceholder: "描述剧情，如“少年在雨夜的车站等待一列迟到的末班车”",
		Ratios:            []string{"16:9", "4:3", "1:1"},
	},
	{
		Slug: "id-photo", Category: "creative", Title: "证件照",
		Description: "上传一张清晰正脸照，生成规范的证件照",
		Inputs:      []model.ImageTemplateInput{{Key: "portrait", Label: "人像照片", Max: 1, Required: true}},
		Prompt: "把 @图片1 中的人物制作成一张标准证件照：正面免冠、头部居中、双肩水平，头顶与画面上沿留出适当空隙；保持本人五官、脸型、发型和肤色真实不变，只做轻微的肤色均匀和光线修正。" +
			"服装换成得体的深色正装，背景为均匀纯色（未指定时用浅蓝色），无阴影、无杂物、无文字。{{prompt}}",
		PromptPlaceholder: "如“蓝色背景”“白色背景，白衬衫”，不填默认浅蓝底",
		Ratios:            []string{"3:4", "4:5"},
	},
	{
		Slug: "photo-booth", Category: "creative", Title: "合照大头贴",
		Description: "上传几个人的照片，生成可爱的大头贴合照",
		Inputs:      []model.ImageTemplateInput{{Key: "people", Label: "人物照片", Max: 4, Required: true}},
		Prompt: "把上传照片中的每个人物放进同一张大头贴风格的合照里：人物凑在一起做出俏皮表情和手势，保持每个人的五官与发型可辨认。" +
			"画面加上明亮的糖果色背景、手绘涂鸦边框、星星和爱心贴纸，整体像拍贴机拍出的照片，色调清新、氛围欢乐。{{prompt}}",
		PromptPlaceholder: "可选：如“加上‘毕业快乐’字样”",
		Ratios:            []string{"3:4", "1:1", "4:3"},
	},
	{
		Slug: "festival-poster", Category: "creative", Title: "节日海报",
		Description: "选一个节日，生成氛围感十足的节日主题海报",
		Inputs:      []model.ImageTemplateInput{{Key: "subject", Label: "主体图片", Max: 1}},
		Prompt: "设计一张节日主题海报，主题与文案：{{prompt}}。运用该节日最有代表性的色彩、元素和氛围营造画面，构图完整、主次分明，节日文字醒目准确、无错别字。" +
			"若提供了「主体图片」，把其中的主体自然融入画面中心并保持外观不变。",
		PromptRequired:    true,
		PromptPlaceholder: "如“中秋节，花好月圆人团圆”",
		Ratios:            []string{"3:4", "9:16", "1:1", "16:9"},
	},
	{
		Slug: "packaging-design", Category: "creative", Title: "产品包装设计",
		Description: "描述产品与风格，生成包装盒、瓶身等包装效果图",
		Inputs:      []model.ImageTemplateInput{{Key: "brand", Label: "产品或 Logo", Max: 2}},
		Prompt: "为下述产品设计包装并输出写实的包装效果图：{{prompt}}。包装形态与材质符合品类常见规范，正面信息层级清晰，图形与配色有记忆点；" +
			"若提供了「产品或 Logo」，在包装上准确使用其中的 Logo 或产品形象，不要改动 Logo 字形。展示时采用干净的棚拍背景与柔和光影，可同时呈现正面和侧面。",
		PromptRequired:    true,
		PromptPlaceholder: "如“一款柚子味气泡水，清爽年轻的国潮风罐装”",
		Ratios:            []string{"1:1", "4:3", "3:4"},
	},
	{
		Slug: "photo-restore", Category: "creative", Title: "旧照修复",
		Description: "修复划痕、褪色和模糊，让老照片焕然一新",
		Inputs:      []model.ImageTemplateInput{{Key: "photo", Label: "老照片", Max: 1, Required: true}},
		Prompt: "修复 @图片1 这张老照片：去除划痕、折痕、污渍和噪点，补全缺损区域，提升清晰度和细节；保持人物五官、表情、服饰和原始构图完全不变，不要美颜或改变年龄。" +
			"未另行说明时还原自然、真实的色彩。{{prompt}}",
		PromptPlaceholder: "如“自然上色”或“保持黑白”，不填则还原自然色彩",
		Ratios:            []string{"1:1", "3:4", "4:3", "2:3", "3:2"},
	},
}

// EnsureDefaultImageTemplates seeds the starter image templates as drafts when the table is empty,
// and the video ones when there is no video template yet.
func EnsureDefaultImageTemplates() error {
	current := time.Now().UTC()
	items := make([]model.ImageTemplate, len(defaultImageTemplates))
	for i, item := range defaultImageTemplates {
		item.ID, item.Kind, item.Status, item.Sort, item.MemberFree = newID("imgtpl"), model.ImageTemplateKindImage, model.ImageTemplateStatusDraft, (i+1)*10, true
		item.Resolutions, item.Examples, item.CreatedAt, item.UpdatedAt = allResolutions, []model.ImageTemplateExample{}, current, current
		if item.Inputs == nil {
			item.Inputs = []model.ImageTemplateInput{}
		}
		items[i] = item
	}
	if err := repository.SeedImageTemplates("", items); err != nil {
		return err
	}
	videos := make([]model.ImageTemplate, len(defaultVideoTemplates))
	for i, item := range defaultVideoTemplates {
		defaults := *item.VideoDefaults
		item.Kind, item.Status, item.VideoDefaults = model.ImageTemplateKindVideo, model.ImageTemplateStatusDraft, &defaults
		if err := normalizeVideoTemplate(&item); err != nil {
			return err
		}
		item.ID, item.Sort, item.CreatedAt, item.UpdatedAt = newID("imgtpl"), (i+1)*10, current, current
		videos[i] = item
	}
	return repository.SeedImageTemplates(model.ImageTemplateKindVideo, videos)
}
