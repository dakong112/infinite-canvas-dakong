package service

import "github.com/tigerowo/infinite-canvas/model"

type videoShot = model.VideoTemplateShot

// defaultVideoTemplates prefill the product video wizard; seeded as drafts like the image ones.
var defaultVideoTemplates = []model.ImageTemplate{
	{
		Slug: "video-gadget-unboxing", Category: "video-unboxing", Title: "数码好物开箱",
		Description: "第一人称拆箱，展示包装、外观细节与上手体验，适合耳机、充电宝等小数码",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 30, Ratio: "9:16", Style: "unboxing", Name: "无线降噪耳机",
			Intro: "入耳式真无线蓝牙耳机，充电仓小巧可放口袋，支持主动降噪与通透模式。", SellingPoints: "主动降噪，通勤安静\n单次续航 8 小时\n佩戴轻盈不压耳",
			Shots: []videoShot{
				{Seconds: 4, Scene: "桌面上放着未拆封的耳机包装盒，一双手拿起盒子展示正面", Camera: "缓慢推近", Voiceover: "今天开箱这款降噪耳机", Subtitle: "降噪耳机开箱"},
				{Seconds: 5, Scene: "双手撕开封条，打开盒盖，充电仓与配件整齐摆放在内托中", Camera: "俯拍固定机位", Voiceover: "包装很简洁，配件一应俱全", Subtitle: "配件齐全"},
				{Seconds: 5, Scene: "手指打开充电仓，取出耳机，展示耳机外观与磨砂质感", Camera: "微距特写", Voiceover: "仓盖手感顺滑，耳机小巧", Subtitle: "小巧轻盈"},
				{Seconds: 5, Scene: "年轻人在地铁车厢里戴上耳机，周围嘈杂而他神情放松", Camera: "平移跟拍", Voiceover: "一键降噪，地铁里也很安静", Subtitle: "主动降噪"},
				{Seconds: 5, Scene: "耳机放在充电仓旁，充电指示灯亮起，旁边是手机电量界面", Camera: "缓慢环绕", Voiceover: "单次续航八小时，一天够用", Subtitle: "续航 8 小时"},
				{Seconds: 5, Scene: "耳机与充电仓摆在干净的桌面中央，柔和光线，商品清晰突出", Camera: "缓慢拉远", Voiceover: "通勤党闭眼入，链接在下方", Subtitle: "点击下方购买"},
			},
		},
	},
	{
		Slug: "video-beauty-unboxing", Category: "video-unboxing", Title: "美妆护肤开箱",
		Description: "快节奏开箱 + 质地展示 + 上脸效果，适合精华、面霜等护肤品",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 15, Ratio: "9:16", Style: "unboxing", Name: "玻尿酸保湿精华",
			Intro: "清爽水润的保湿精华液，30ml 滴管瓶，适合干皮与混合皮日常使用。", SellingPoints: "一抹吸收不黏腻\n补水保湿一整天\n敏感肌可用",
			Shots: []videoShot{
				{Seconds: 3, Scene: "粉色背景前，一只手打开精华液包装盒，取出滴管瓶", Camera: "缓慢推近", Voiceover: "新入手的保湿精华", Subtitle: "保湿精华开箱"},
				{Seconds: 4, Scene: "滴管吸取精华，液滴落在手背上，质地清透有光泽", Camera: "微距特写", Voiceover: "质地清透，一抹就化开", Subtitle: "清透不黏腻"},
				{Seconds: 4, Scene: "女生将精华轻拍在脸颊上，皮肤水润透亮", Camera: "固定机位近景", Voiceover: "上脸很快吸收，水润一整天", Subtitle: "补水一整天"},
				{Seconds: 4, Scene: "精华瓶立在白色台面上，旁边点缀水滴与绿植，光线明亮", Camera: "缓慢环绕", Voiceover: "干皮姐妹冲，链接在下方", Subtitle: "点击下方购买"},
			},
		},
	},
	{
		Slug: "video-restaurant-visit", Category: "video-local", Title: "餐饮门店探店",
		Description: "门头 → 环境 → 招牌菜 → 优惠信息，吸引附近顾客到店",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 30, Ratio: "9:16", Style: "scene", Name: "老街手工牛肉面",
			Intro: "开在社区老街的面馆，每天现熬牛骨汤，手工拉面现点现做，人均 25 元。", SellingPoints: "牛骨汤熬足 8 小时\n面条现拉劲道\n到店出示视频送卤蛋",
			Shots: []videoShot{
				{Seconds: 5, Scene: "傍晚的老街，面馆暖黄色的招牌亮着，门口有顾客排队", Camera: "缓慢推近门头", Voiceover: "藏在老街的这家面馆，天天排队", Subtitle: "老街宝藏面馆"},
				{Seconds: 5, Scene: "店内木质桌椅干净整洁，顾客边吃边聊，氛围热闹温馨", Camera: "平移", Voiceover: "店面不大，但很干净", Subtitle: "干净温馨"},
				{Seconds: 5, Scene: "后厨大锅里牛骨汤翻滚，热气升腾", Camera: "俯拍推近", Voiceover: "牛骨汤每天现熬八小时", Subtitle: "现熬 8 小时"},
				{Seconds: 5, Scene: "师傅手工拉面，面条在空中甩开，动作利落", Camera: "固定机位", Voiceover: "面条现点现拉，特别劲道", Subtitle: "手工现拉"},
				{Seconds: 5, Scene: "一碗牛肉面端上桌，大块牛肉铺满，筷子挑起面条", Camera: "微距特写", Voiceover: "牛肉给得很实在，汤也鲜", Subtitle: "人均 25 元"},
				{Seconds: 5, Scene: "门店外景与店名招牌，夜色中灯光温暖", Camera: "缓慢拉远", Voiceover: "到店出示视频送卤蛋，快来吧", Subtitle: "到店送卤蛋"},
			},
		},
	},
	{
		Slug: "video-studio-trial", Category: "video-local", Title: "到店体验课",
		Description: "展示环境、服务过程与体验价，适合健身、美甲、美发等到店服务",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 15, Ratio: "9:16", Style: "scene", Name: "社区私教健身体验课",
			Intro: "社区里的小型健身工作室，一对一私教指导，器械齐全，提供淋浴间。", SellingPoints: "新人体验课 39 元\n一对一私教\n离家 5 分钟",
			Shots: []videoShot{
				{Seconds: 4, Scene: "明亮的健身工作室全景，器械整齐，落地窗采光充足", Camera: "缓慢推近", Voiceover: "家门口的私教工作室", Subtitle: "家门口的健身房"},
				{Seconds: 4, Scene: "教练在一旁指导学员做深蹲，耐心纠正动作", Camera: "环绕", Voiceover: "一对一指导，动作更标准", Subtitle: "一对一私教"},
				{Seconds: 3, Scene: "学员训练后擦汗微笑，与教练击掌", Camera: "固定机位", Voiceover: "练完特别有成就感", Subtitle: "练出好状态"},
				{Seconds: 4, Scene: "前台桌面上摆着体验课海报与预约二维码", Camera: "缓慢拉远", Voiceover: "新人体验课只要三十九", Subtitle: "体验课 39 元"},
			},
		},
	},
	{
		Slug: "video-home-cleaning", Category: "video-service", Title: "上门保洁服务",
		Description: "清洁前后对比 + 服务流程，突出专业与放心，适合家政保洁",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 30, Ratio: "9:16", Style: "demo", Name: "全屋深度保洁",
			Intro: "专业保洁师上门，自带工具与环保清洁剂，覆盖厨房、卫生间、客厅、卧室。", SellingPoints: "厨房重油污一次清干净\n工具消毒、环保药剂\n不满意免费返工",
			Shots: []videoShot{
				{Seconds: 5, Scene: "保洁师穿着统一工服、戴着鞋套，提着工具箱按门铃", Camera: "固定机位", Voiceover: "预约的保洁师准时上门", Subtitle: "准时上门"},
				{Seconds: 5, Scene: "厨房灶台油污特写，随后保洁师喷上清洁剂擦拭", Camera: "微距推近", Voiceover: "厨房重油污是重点", Subtitle: "重油污克星"},
				{Seconds: 5, Scene: "擦拭后的灶台光亮如新，与之前形成鲜明对比", Camera: "平移", Voiceover: "擦完直接光亮如新", Subtitle: "焕然一新"},
				{Seconds: 5, Scene: "保洁师清洁卫生间玻璃与地面，水渍全部清除", Camera: "跟拍", Voiceover: "卫生间玻璃也没有水印", Subtitle: "细节到位"},
				{Seconds: 5, Scene: "整洁明亮的客厅，阳光照进来，地面反光干净", Camera: "缓慢环绕", Voiceover: "全屋清洁完，住着都舒心", Subtitle: "全屋焕新"},
				{Seconds: 5, Scene: "保洁师微笑递上服务单，背景是干净的家", Camera: "缓慢拉远", Voiceover: "不满意免费返工，点击预约", Subtitle: "点击下方预约"},
			},
		},
	},
	{
		Slug: "video-ac-cleaning", Category: "video-service", Title: "家电清洗维修",
		Description: "拆洗过程可视化，脏污对比一目了然，适合空调、油烟机清洗",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 15, Ratio: "16:9", Style: "demo", Name: "空调深度拆洗",
			Intro: "师傅上门拆洗挂机空调，高温蒸汽杀菌，清洗后风更凉、更干净。", SellingPoints: "高温蒸汽杀菌\n拆洗全程可见\n一台 99 元起",
			Shots: []videoShot{
				{Seconds: 4, Scene: "师傅拆下空调面板，露出积满灰尘的滤网与散热片", Camera: "推近特写", Voiceover: "空调一年不洗有多脏", Subtitle: "看看有多脏"},
				{Seconds: 4, Scene: "高温蒸汽喷向散热片，污水顺着接水袋流下", Camera: "微距", Voiceover: "高温蒸汽深层杀菌", Subtitle: "蒸汽杀菌"},
				{Seconds: 3, Scene: "清洗后的散热片银亮干净，与之前形成对比", Camera: "平移", Voiceover: "洗完干干净净", Subtitle: "干净如新"},
				{Seconds: 4, Scene: "装好的空调送出凉风，一家人在客厅舒适休息", Camera: "缓慢拉远", Voiceover: "一台九十九起，马上预约", Subtitle: "99 元起"},
			},
		},
	},
	{
		Slug: "video-festival-gift", Category: "video-promo", Title: "节日礼盒促销",
		Description: "节日氛围 + 礼盒开合 + 限时优惠，适合中秋、春节等送礼季",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 15, Ratio: "9:16", Style: "promo", Name: "中秋每日坚果礼盒",
			Intro: "8 种坚果果干独立小袋装，烫金礼盒配手提袋，送长辈送客户都体面。", SellingPoints: "8 种坚果每日一袋\n烫金礼盒有面子\n限时第二件半价",
			Shots: []videoShot{
				{Seconds: 3, Scene: "红金色节日背景，礼盒在灯笼与月亮装饰中缓缓出现", Camera: "推近", Voiceover: "中秋送礼选它", Subtitle: "中秋送礼"},
				{Seconds: 4, Scene: "双手打开礼盒，一排排坚果小袋整齐排列", Camera: "俯拍", Voiceover: "八种坚果，每天一袋", Subtitle: "8 种坚果"},
				{Seconds: 4, Scene: "坚果倒入白瓷盘，颗粒饱满，旁边一家人围坐赏月", Camera: "环绕", Voiceover: "颗颗饱满，全家都爱吃", Subtitle: "颗颗饱满"},
				{Seconds: 4, Scene: "礼盒与手提袋并排摆放，画面充满节日促销氛围", Camera: "快速推近", Voiceover: "限时第二件半价，快下单", Subtitle: "第二件半价"},
			},
		},
	},
	{
		Slug: "video-flash-sale", Category: "video-promo", Title: "店庆限时快剪",
		Description: "短镜头快切，突出价格与限时氛围，适合店庆、大促引流",
		VideoDefaults: &model.VideoTemplateDefaults{
			Duration: 15, Ratio: "1:1", Style: "promo", Name: "不锈钢保温杯",
			Intro: "316 不锈钢内胆保温杯，500ml，12 小时保温，多色可选。", SellingPoints: "12 小时长效保温\n316 不锈钢内胆\n店庆价 59 元",
			Shots: []videoShot{
				{Seconds: 3, Scene: "多色保温杯在彩色背景前依次弹出排列", Camera: "快速推近", Voiceover: "店庆大促来了", Subtitle: "店庆大促"},
				{Seconds: 3, Scene: "热水倒入保温杯，杯口冒出热气", Camera: "微距特写", Voiceover: "十二小时长效保温", Subtitle: "保温 12 小时"},
				{Seconds: 3, Scene: "杯盖拧开展示内胆，金属光泽清晰", Camera: "环绕", Voiceover: "三一六不锈钢内胆", Subtitle: "316 内胆"},
				{Seconds: 3, Scene: "上班族在办公桌前拿起保温杯喝水，神情轻松", Camera: "固定机位", Voiceover: "办公出行都好用", Subtitle: "随身好用"},
				{Seconds: 3, Scene: "保温杯居中特写，背景是节日彩带与闪光", Camera: "缓慢拉远", Voiceover: "店庆价五十九，手慢无", Subtitle: "店庆价 59 元"},
			},
		},
	},
}
