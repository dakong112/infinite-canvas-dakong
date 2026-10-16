package service

import (
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// EnsureDefaultAcademyCourses seeds four draft sample courses while the academy is empty.
func EnsureDefaultAcademyCourses() error {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(n int) *time.Time {
		at := now.AddDate(0, 0, n)
		return &at
	}
	lessons := func(titles ...string) []model.AcademyLesson {
		items := make([]model.AcademyLesson, len(titles))
		for i, title := range titles {
			items[i] = model.AcademyLesson{Title: title}
		}
		return items
	}
	items := []model.AcademyCourse{
		{Slug: "ai-product-image-camp", Kind: model.AcademyKindCamp, Category: "AI技能提升", Title: "AI 电商主图训练营",
			Summary:  "10 天从零做出能直接上架的商品主图：白底图、场景图、卖点图一次学会，结营交付一套完整店铺主图。",
			Body:     "## 你将学到\n- 商品抠图与白底图规范\n- 场景图提示词结构与光影控制\n- 卖点图排版与文字生成\n\n## 适合人群\n电商店主、美工、想用 AI 提效的运营。\n\n## 交付\n每位学员结营时提交一套 5 张主图，导师逐一点评。",
			Lessons:  lessons("开营：主图的平台规范", "白底图与抠图实操", "场景图提示词拆解", "卖点图与文字排版", "结营作业点评"),
			StartsAt: day(7), EndsAt: day(17), EnrollEndsAt: day(6), Capacity: 200, MemberOnly: true, FeeCredits: 99, MemberFree: true, Sort: 10},
		{Slug: "selling-video-7-days", Kind: model.AcademyKindCamp, Category: "AI项目变现", Title: "带货短视频 7 天营",
			Summary:  "7 天跑通一条带货短视频流水线：选品、脚本、分镜、AI 生成到剪辑发布，每天一个可交付的小成果。",
			Body:     "## 课程安排\n第 1 天选品与卖点提炼，第 2-3 天脚本与分镜，第 4-5 天 AI 生成镜头，第 6 天剪辑配音，第 7 天发布与复盘。\n\n## 你需要准备\n一个想推广的商品和每天 1 小时的练习时间。",
			Lessons:  lessons("第 1 天：选品与卖点", "第 2 天：带货脚本公式", "第 3 天：分镜设计", "第 4 天：AI 生成镜头", "第 5 天：镜头补拍与替换", "第 6 天：剪辑与配音", "第 7 天：发布与数据复盘"),
			StartsAt: day(10), EndsAt: day(17), EnrollEndsAt: day(9), Capacity: 100, FeeCredits: 199, MemberFree: true, Sort: 20},
		{Slug: "template-library-course", Kind: model.AcademyKindCourse, Category: "AI技能提升", Title: "模板库玩法课",
			Summary: "用好模板库的每一个入口：挑模板、改提示词、批量出图，再把自己的好结果存成模板反复使用。",
			Body:    "## 内容\n- 模板分类与选择思路\n- 改写提示词而不破坏模板效果\n- 批量出图与结果挑选\n- 把作品另存为自己的模板\n\n随到随学，无名额限制。",
			Lessons: lessons("模板库导览", "改写模板提示词", "批量出图技巧", "另存为我的模板"),
			Sort:    30},
		{Slug: "team-collaboration-basics", Kind: model.AcademyKindCourse, Category: "团队协作", Title: "团队协作入门",
			Summary:  "带着团队一起用无限画布：切换团队空间、共享画布与作品、分配额度，一节课讲清楚团队怎么协作。",
			Body:     "## 内容\n- 个人空间与团队空间的区别\n- 共享画布项目与作品\n- 团队钱包与成员额度\n- 常见协作问题答疑",
			Lessons:  lessons("个人空间与团队空间", "共享画布与作品", "团队钱包与成员额度"),
			StartsAt: day(3), EndsAt: day(3), Capacity: 300, MemberOnly: true, Sort: 40},
	}
	for i := range items {
		items[i].Status, items[i].CreatedAt, items[i].UpdatedAt = model.AcademyStatusDraft, now, now
	}
	return repository.SeedAcademyCourses(items)
}
