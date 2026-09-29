// Package evaldata REQ-194/M34 批次二④：伴生召回评估基准资产。
// 种子会话 3 段（运维/医学/通用，刻意埋同义改述与跨距离回指——正对 G1 隐式召回失败场景）
// + 固定 stub 事实面（只评测召回环节，抽取质量另行走人工抽检口径）+ 问题集 15 条
// （每条标注期望命中实体）。跑分器见 eval_test.go（build tag `eval` 手动跑，不进 CI 硬门禁
// ——依赖 embedding 连接与 oxigraph 二进制在位）。
//
// 数据形态约定：事实面（Facts）是评测的唯一图数据来源——种子会话轮次（Turns）为事实的
// 溯源语境（模拟"对话中确证的领域事实"），评测环节不消费轮次文本；问题集的期望实体
// 以事实面的标签原文为准（对齐评测口径：命中=检索实体 ∩ 期望实体）。
package evaldata

// Turn 一轮对话（溯源语境）。
type Turn struct {
	Role    string // user | assistant
	Content string
}

// Fact 固定 stub 事实（替代 LLM 抽取产物——隔离抽取质量变量，只测召回）。
type Fact struct {
	Kind       string // concept | relation | event
	Name       string // 概念/事件标签；relation 时为主体现
	RelName    string // relation 专用
	RelTarget  string // relation 专用
	Definition string `json:"definition,omitempty"`
	Confidence float64
}

// Question 一条评测问题：期望命中实体 + 意图注记（同义改述/跨距离回指标注）。
type Question struct {
	ID       string
	Conv     string   // 归属会话
	Input    string   // 用户问题原文
	Expected []string // 期望命中实体标签（Recall@5 分子分母以此为准）
	Intent   string   // 考点注记（同义改述/直接指名/跨距离回指）
}

// Session 一段种子会话。
type Session struct {
	ID     string
	Domain string // 运维 | 医学 | 通用
	Turns  []Turn
	Facts  []Fact
}

// Sessions 三段种子会话（每段刻意埋同义改述：事实用术语 A 陈述，问题用改述 B 提问——
// 词法包含零召回、向量召回命中的对照场景）。
var Sessions = []Session{
	{
		ID: "eval-ops", Domain: "运维",
		Facts: []Fact{
			{Kind: "concept", Name: "Pod 水平扩容", Definition: "按负载增加 Pod 副本数的伸缩动作", Confidence: 0.9},
			{Kind: "concept", Name: "HPA 控制器", Definition: "依据指标自动调整副本数的控制器", Confidence: 0.88},
			{Kind: "concept", Name: "滚动更新", Definition: "逐批替换旧实例的发布方式", Confidence: 0.9},
			{Kind: "concept", Name: "就绪探针", Definition: "判定实例可接流的健康检查", Confidence: 0.87},
			{Kind: "concept", Name: "金丝雀发布", Definition: "小流量先行验证的发布策略", Confidence: 0.85},
			{Kind: "concept", Name: "服务熔断", Definition: "依赖故障时快速失败的保护机制", Confidence: 0.86},
			{Kind: "concept", Name: "配置中心", Definition: "应用配置集中管理与服务端", Confidence: 0.84},
			{Kind: "concept", Name: "镜像仓库", Definition: "容器镜像的存储与分发服务", Confidence: 0.83},
			{Kind: "relation", Name: "Pod 水平扩容", RelName: "由", RelTarget: "HPA 控制器", Definition: "扩缩容动作由 HPA 依据指标驱动", Confidence: 0.87},
			{Kind: "relation", Name: "滚动更新", RelName: "依赖", RelTarget: "就绪探针", Definition: "滚动更新按就绪探针判定批次推进", Confidence: 0.86},
			{Kind: "relation", Name: "金丝雀发布", RelName: "先于", RelTarget: "滚动更新", Definition: "金丝雀验证通过后再全量滚动", Confidence: 0.8},
			{Kind: "relation", Name: "镜像仓库", RelName: "供给", RelTarget: "滚动更新", Definition: "滚动更新从镜像仓库拉取新版本", Confidence: 0.78},
		},
		Turns: []Turn{
			{"user", "大促流量要来了，Pod 撑不住怎么办？"},
			{"assistant", "开 HPA 控制器，指标超阈值会自动执行 Pod 水平扩容，副本数跟着负载走。"},
			{"user", "扩容是自动的？还是要人工加机器？"},
			{"assistant", "HPA 驱动的 Pod 水平扩容是全自动的，不需要人工加机器。"},
			{"user", "发新版本不想停服，有什么方式？"},
			{"assistant", "用滚动更新：逐批替换旧实例，全程不停服；更稳一点先走金丝雀发布，小流量验证后再全量。"},
			{"user", "滚动更新怎么保证替换过程中的实例是好的？"},
			{"assistant", "靠就绪探针：新实例就绪探针通过才会接入流量，滚动更新按批次推进。"},
			{"user", "依赖的下游服务挂了怎么办？"},
			{"assistant", "上服务熔断：依赖持续失败时快速失败并降级，防止故障拖垮自身。"},
			{"user", "应用配置散在各处不好管。"},
			{"assistant", "收敛到配置中心：配置集中管理、动态下发，改配置不用重启实例。"},
			{"user", "镜像存在哪里比较好？"},
			{"assistant", "统一推镜像仓库，滚动更新和扩容都从仓库拉取，版本可追溯。"},
		},
	},
	{
		ID: "eval-med", Domain: "医学",
		Facts: []Fact{
			{Kind: "concept", Name: "阿司匹林", Definition: "非甾体抗炎药，抗血小板聚集", Confidence: 0.92},
			{Kind: "concept", Name: "前列腺素", Definition: "介导炎症与疼痛信号的脂质活性物", Confidence: 0.85},
			{Kind: "concept", Name: "血栓形成", Definition: "血管内血液凝块生成的病理过程", Confidence: 0.88},
			{Kind: "concept", Name: "心肌梗死", Definition: "冠状动脉闭塞致心肌缺血坏死", Confidence: 0.9},
			{Kind: "concept", Name: "胃黏膜损伤", Definition: "胃黏膜屏障受损引发的病变", Confidence: 0.84},
			{Kind: "concept", Name: "他汀类药物", Definition: "降胆固醇的一线调脂药", Confidence: 0.89},
			{Kind: "concept", Name: "低密度脂蛋白", Definition: "致动脉粥样硬化的脂蛋白", Confidence: 0.86},
			{Kind: "concept", Name: "溶栓治疗", Definition: "药物溶解血栓的再灌注手段", Confidence: 0.87},
			{Kind: "relation", Name: "阿司匹林", RelName: "抑制", RelTarget: "前列腺素", Definition: "阿司匹林抑制前列腺素合成", Confidence: 0.9},
			{Kind: "relation", Name: "阿司匹林", RelName: "预防", RelTarget: "血栓形成", Definition: "抗血小板作用降低血栓风险", Confidence: 0.88},
			{Kind: "relation", Name: "血栓形成", RelName: "导致", RelTarget: "心肌梗死", Definition: "冠脉血栓闭塞可致心肌梗死", Confidence: 0.89},
			{Kind: "relation", Name: "阿司匹林", RelName: "引发", RelTarget: "胃黏膜损伤", Definition: "抑制前列腺素削弱胃黏膜屏障", Confidence: 0.85},
			{Kind: "relation", Name: "他汀类药物", RelName: "降低", RelTarget: "低密度脂蛋白", Definition: "他汀降低低密度脂蛋白胆固醇", Confidence: 0.9},
			{Kind: "relation", Name: "溶栓治疗", RelName: "用于", RelTarget: "心肌梗死", Definition: "急性期药物再灌注手段", Confidence: 0.86},
		},
		Turns: []Turn{
			{"user", "阿司匹林为什么能抗炎镇痛？"},
			{"assistant", "阿司匹林抑制前列腺素合成，而前列腺素介导炎症与疼痛信号。"},
			{"user", "它对血管有什么好处？"},
			{"assistant", "抗血小板聚集，能预防血栓形成，心脑血管风险人群常用小剂量。"},
			{"user", "为什么吃阿司匹林有的人胃不舒服？"},
			{"assistant", "前列腺素也维护胃黏膜屏障，被抑制后可能出现胃黏膜损伤。"},
			{"user", "血脂高主要看哪个指标？"},
			{"assistant", "重点看低密度脂蛋白，它是致动脉粥样硬化的主因。"},
			{"user", "降这个指标用什么药？"},
			{"assistant", "他汀类药物是一线：显著降低低密度脂蛋白胆固醇。"},
			{"user", "血管完全堵住会发生什么？"},
			{"assistant", "血栓形成完全阻断血流，供给区域缺血坏死——冠脉即心肌梗死。"},
			{"user", "堵住之后还有救吗？"},
			{"assistant", "急性期可行溶栓治疗，药物溶解血栓恢复再灌注。"},
		},
	},
	{
		ID: "eval-general", Domain: "通用",
		Facts: []Fact{
			{Kind: "concept", Name: "每日站会", Definition: "团队短会同步进展与阻塞", Confidence: 0.86},
			{Kind: "concept", Name: "代码评审", Definition: "合并前同行检查代码的实践", Confidence: 0.9},
			{Kind: "concept", Name: "结对编程", Definition: "两人共用一机协作编码", Confidence: 0.82},
			{Kind: "concept", Name: "技术债", Definition: "为短期交付欠下的返工成本", Confidence: 0.88},
			{Kind: "concept", Name: "知识沉淀", Definition: "经验文档化入库的实践", Confidence: 0.85},
			{Kind: "concept", Name: "灰度发布", Definition: "按流量比例逐步放量的发布", Confidence: 0.87},
			{Kind: "concept", Name: "用户访谈", Definition: "定性收集用户诉求的方法", Confidence: 0.84},
			{Kind: "relation", Name: "代码评审", RelName: "减少", RelTarget: "技术债", Definition: "评审在合入前拦截劣化", Confidence: 0.83},
			{Kind: "relation", Name: "结对编程", RelName: "加速", RelTarget: "知识沉淀", Definition: "新人经结对快速吸收上下文", Confidence: 0.8},
			{Kind: "relation", Name: "用户访谈", RelName: "输入", RelTarget: "知识沉淀", Definition: "访谈结论归档为领域知识", Confidence: 0.76},
		},
		Turns: []Turn{
			{"user", "团队每天对齐进展用什么形式？"},
			{"assistant", "每日站会：15 分钟内同步昨天/今天/阻塞三件事。"},
			{"user", "怎么保证代码质量不滑坡？"},
			{"assistant", "坚持代码评审：合入前同行检查，评审是技术债的第一道闸。"},
			{"user", "新人来了怎么带比较好？"},
			{"assistant", "结对编程最有效：真实任务里手把手带，顺手完成知识沉淀。"},
			{"user", "历史上欠的返工怎么治理？"},
			{"assistant", "先盘点技术债并登记，排进迭代按利息高低偿还。"},
			{"user", "新功能直接全量上线风险大。"},
			{"assistant", "走灰度发布：按流量比例逐步放量，异常即回滚。"},
			{"user", "需求来源太散怎么收敛？"},
			{"assistant", "定期做用户访谈，结论沉淀进需求池统一评审。"},
		},
	},
}

// Questions 15 条评测问题（每域 5 条；同义改述题词法包含零召回，向量为对照组考点）。
var Questions = []Question{
	// —— 运维（eval-ops）——
	{ID: "Q01", Conv: "eval-ops", Input: "流量突增时怎么自动加实例？", Expected: []string{"Pod 水平扩容", "HPA 控制器"}, Intent: "同义改述：加实例→水平扩容；自动→HPA"},
	{ID: "Q02", Conv: "eval-ops", Input: "新版本上线不停服的办法？", Expected: []string{"滚动更新", "金丝雀发布"}, Intent: "同义改述：上线→发布/更新"},
	{ID: "Q03", Conv: "eval-ops", Input: "怎么避免流量打到还没准备好的实例上？", Expected: []string{"就绪探针"}, Intent: "同义改述：准备好→就绪"},
	{ID: "Q04", Conv: "eval-ops", Input: "下游依赖挂了怎么防止拖垮自己？", Expected: []string{"服务熔断"}, Intent: "同义改述：拖垮→熔断保护"},
	{ID: "Q05", Conv: "eval-ops", Input: "滚动更新从哪里拉新版本镜像？", Expected: []string{"镜像仓库", "滚动更新"}, Intent: "直接指名 + 关系客体（词法基线可命中）"},

	// —— 医学（eval-med）——
	{ID: "Q06", Conv: "eval-med", Input: "哪种药能防止血管里形成血块？", Expected: []string{"阿司匹林", "血栓形成"}, Intent: "同义改述：血块→血栓；问药→阿司匹林"},
	{ID: "Q07", Conv: "eval-med", Input: "心脏血管完全堵死会出什么后果？", Expected: []string{"心肌梗死"}, Intent: "同义改述：心脏血管堵死→心肌梗死"},
	{ID: "Q08", Conv: "eval-med", Input: "长期吃阿司匹林为什么会伤胃？", Expected: []string{"胃黏膜损伤"}, Intent: "主体词法命中 + 客体同义（伤胃→胃黏膜损伤）"},
	{ID: "Q09", Conv: "eval-med", Input: "降胆固醇应该用什么药？", Expected: []string{"他汀类药物"}, Intent: "同义改述：降胆固醇→降脂"},
	{ID: "Q10", Conv: "eval-med", Input: "血管堵了之后紧急打通的手段是什么？", Expected: []string{"溶栓治疗"}, Intent: "同义改述：打通→溶栓"},

	// —— 通用（eval-general）——
	{ID: "Q11", Conv: "eval-general", Input: "团队每天同步进展的短会叫什么？", Expected: []string{"每日站会"}, Intent: "同义改述：短会→站会"},
	{ID: "Q12", Conv: "eval-general", Input: "上线前让别人帮忙看代码有什么讲究？", Expected: []string{"代码评审"}, Intent: "同义改述：帮忙看代码→评审"},
	{ID: "Q13", Conv: "eval-general", Input: "新人快速熟悉项目的带法？", Expected: []string{"结对编程", "知识沉淀"}, Intent: "同义改述：带新人→结对"},
	{ID: "Q14", Conv: "eval-general", Input: "历史欠的返工成本怎么治理？", Expected: []string{"技术债"}, Intent: "同义改述：欠的返工成本→技术债"},
	{ID: "Q15", Conv: "eval-general", Input: "新功能怎么小流量慢慢放开？", Expected: []string{"灰度发布"}, Intent: "同义改述：小流量放开→灰度"},
}
