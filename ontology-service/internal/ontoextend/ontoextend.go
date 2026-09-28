package ontoextend

// ---------------------------------------------------------------------------
// M-O14 P2②（REQ-171 P2 批次 / 26 号方案 §9）：OntoExtend 路径兑现——ODP 精选推荐。
// ODP（Ontology Design Pattern）可发现性是业界未解决问题（docs/23 §2.2），26 号方案 §11
// 决策点 2 拍板：人工精选高频模式 seed（编译期内嵌，不做自动发现；推荐准确率不佳则后置）。
// 片段为 spec_json 形态（概念/关系），扩展草稿经既有 merge/preview|apply 审查底座（REQ-157）
// 入库——冲突检测/改名策略/strict 质量门禁/版本快照全部复用，零新表零旁路。
// ---------------------------------------------------------------------------

import pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"

// ODP 精选模式（人工精选 12 个高频域；source 记出处便于学习对照）
type ODP struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Spec        *pkgspec.Spec `json:"spec"`
}

var curated = []*ODP{
	{ID: "org-person", Name: "组织与人员", Description: "部门/岗位/任职/汇报线——企业组织结构的通用骨架", Source: "器官化模式（Organizational Pattern，W3C org 融汇）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "组织", Label: "Organization", Definition: "法人或虚拟实体（公司/部门/团队）"},
			{Name: "部门", Label: "Department", Definition: "组织的内设单元", Parents: []string{"组织"}},
			{Name: "岗位", Label: "Position", Definition: "组织内设的职责职位"},
			{Name: "人员", Label: "Person", Definition: "自然人"},
			{Name: "任职关系", Label: "Employment", Definition: "人员在某组织担任某岗位的雇佣事实"},
		},
		[]pkgspec.Relation{
			{Name: "隶属于", Label: "partOf", From: "部门", To: "组织"},
			{Name: "设立于", Label: "hostedIn", From: "岗位", To: "部门"},
			{Name: "任职于", Label: "worksAt", From: "人员", To: "岗位"},
			{Name: "汇报给", Label: "reportsTo", From: "人员", To: "人员"},
		})},
	{ID: "defect-mgmt", Name: "软件缺陷管理", Description: "缺陷/严重度/生命周期状态/指派修复流", Source: "软件工程域模式（Bug Tracking）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "缺陷", Label: "Defect", Definition: "软件中不符合预期的行为记录"},
			{Name: "严重度", Label: "Severity", Definition: "缺陷影响的分级（致命/严重/一般/轻微）"},
			{Name: "缺陷状态", Label: "DefectStatus", Definition: "生命周期状态（新建/处理中/已修复/已验证/关闭）"},
			{Name: "修复人", Label: "Fixer", Definition: "负责修复缺陷的人员"},
		},
		[]pkgspec.Relation{
			{Name: "具有严重度", Label: "hasSeverity", From: "缺陷", To: "严重度"},
			{Name: "处于状态", Label: "hasStatus", From: "缺陷", To: "缺陷状态"},
			{Name: "指派给", Label: "assignedTo", From: "缺陷", To: "修复人"},
		})},
	{ID: "equip-fault", Name: "设备故障", Description: "设备/部件/故障/维修工单/原因链", Source: "设备运维域模式（MRO）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "设备", Label: "Equipment", Definition: "被运维的物理设备"},
			{Name: "部件", Label: "Component", Definition: "设备的组成单元", Parents: []string{"设备"}},
			{Name: "故障", Label: "Fault", Definition: "设备或部件的功能失效事件"},
			{Name: "故障原因", Label: "FaultCause", Definition: "引发故障的根因分类"},
			{Name: "维修工单", Label: "WorkOrder", Definition: "一次维修活动的记录"},
		},
		[]pkgspec.Relation{
			{Name: "发生于", Label: "occursOn", From: "故障", To: "设备"},
			{Name: "根因为", Label: "causedBy", From: "故障", To: "故障原因"},
			{Name: "处置", Label: "handledBy", From: "维修工单", To: "故障"},
		})},
	{ID: "sale-order", Name: "销售订单", Description: "客户/订单/订单行/商品/金额", Source: "电商交易域模式（Ordering）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "客户", Label: "Customer", Definition: "下单主体"},
			{Name: "订单", Label: "Order", Definition: "一次购买的合同记录"},
			{Name: "订单行", Label: "OrderLine", Definition: "订单中的单个商品条目"},
			{Name: "商品", Label: "Product", Definition: "被交易的商品"},
		},
		[]pkgspec.Relation{
			{Name: "下单", Label: "places", From: "客户", To: "订单"},
			{Name: "包含", Label: "includes", From: "订单", To: "订单行"},
			{Name: "指向商品", Label: "refersTo", From: "订单行", To: "商品"},
		})},
	{ID: "release-flow", Name: "发布流程", Description: "版本/构建/部署环境/审批门禁", Source: "DevOps 发布工程模式", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "版本", Label: "Release", Definition: "对外交付的软件版本"},
			{Name: "构建产物", Label: "Artifact", Definition: "版本对应的构建产物"},
			{Name: "部署环境", Label: "Environment", Definition: "运行软件的环境（测试/预发/生产）"},
			{Name: "审批门禁", Label: "ApprovalGate", Definition: "进入下一环境前的审批要求"},
		},
		[]pkgspec.Relation{
			{Name: "产出", Label: "produces", From: "版本", To: "构建产物"},
			{Name: "部署到", Label: "deploysTo", From: "版本", To: "部署环境"},
			{Name: "经过门禁", Label: "passesGate", From: "版本", To: "审批门禁"},
		})},
	{ID: "measure-unit", Name: "度量与单位", Description: "物理量/度量单位/换算关系——科学与企业测量的通用骨架", Source: "QUDT/OM 精简对照", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "物理量", Label: "QuantityKind", Definition: "可测量的维度（长度/质量/时间）"},
			{Name: "度量单位", Label: "UnitOfMeasure", Definition: "物理量的标准化计量单位"},
			{Name: "换算关系", Label: "Conversion", Definition: "单位间的数值换算"},
		},
		[]pkgspec.Relation{
			{Name: "以单位计量", Label: "measuredIn", From: "物理量", To: "度量单位"},
			{Name: "可换算为", Label: "convertibleTo", From: "度量单位", To: "度量单位"},
		})},
	{ID: "time-interval", Name: "时间区间与事件", Description: "时间点/区间/事件排序——时序事实的通用骨架", Source: "OWL-Time 精简对照", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "时间点", Label: "Instant", Definition: "时间轴上的瞬时位置"},
			{Name: "时间区间", Label: "Interval", Definition: "起止时间点界定的区段"},
			{Name: "事件", Label: "Event", Definition: "发生在时间区间内的事实"},
		},
		[]pkgspec.Relation{
			{Name: "始于", Label: "beginsAt", From: "时间区间", To: "时间点"},
			{Name: "止于", Label: "endsAt", From: "时间区间", To: "时间点"},
			{Name: "发生于区间", Label: "occursIn", From: "事件", To: "时间区间"},
			{Name: "先于", Label: "before", From: "事件", To: "事件"},
		})},
	{ID: "contract-party", Name: "合同与当事方", Description: "合同/当事方/角色/条款", Source: "法律信息域模式（LegalDoc）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "合同", Label: "Contract", Definition: "约束多方义务的协议文件"},
			{Name: "当事方", Label: "Party", Definition: "合同签署主体（组织或个人）"},
			{Name: "当事角色", Label: "PartyRole", Definition: "当事方在合同中的角色（甲方/乙方/担保方）"},
			{Name: "条款", Label: "Clause", Definition: "合同中的具体约定条目"},
		},
		[]pkgspec.Relation{
			{Name: "签署", Label: "signs", From: "当事方", To: "合同"},
			{Name: "承担角色", Label: "playsRole", From: "当事方", To: "当事角色"},
			{Name: "包含条款", Label: "hasClause", From: "合同", To: "条款"},
		})},
	{ID: "medical-prescription", Name: "医疗处方", Description: "患者/处方/药品/用法用量（教学示例，非临床用途）", Source: "FHIR MedicationRequest 精简对照", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "患者", Label: "Patient", Definition: "接受诊疗的自然人"},
			{Name: "处方", Label: "Prescription", Definition: "医师开具的用药指令"},
			{Name: "药品", Label: "Medication", Definition: "处方指向的药品"},
			{Name: "用法用量", Label: "Dosage", Definition: "药品的服用方式与剂量"},
		},
		[]pkgspec.Relation{
			{Name: "开具给", Label: "prescribedTo", From: "处方", To: "患者"},
			{Name: "指向药品", Label: "hasMedication", From: "处方", To: "药品"},
			{Name: "携带用法", Label: "hasDosage", From: "处方", To: "用法用量"},
		})},
	{ID: "inventory-warehouse", Name: "库存与仓储", Description: "仓库/货位/物料/库存记录/出入库单", Source: "供应链仓储模式（WMS）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "仓库", Label: "Warehouse", Definition: "存储物料的物理场所"},
			{Name: "货位", Label: "StorageBin", Definition: "仓库内的存储位置单元"},
			{Name: "物料", Label: "Material", Definition: "被存储与流转的物品"},
			{Name: "库存记录", Label: "StockRecord", Definition: "某货位上某物料的数量快照"},
			{Name: "出入库单", Label: "MovementOrder", Definition: "物料移动的凭证"},
		},
		[]pkgspec.Relation{
			{Name: "位于", Label: "locatedIn", From: "货位", To: "仓库"},
			{Name: "记录物料", Label: "recordsMaterial", From: "库存记录", To: "物料"},
			{Name: "存放于", Label: "storedAt", From: "库存记录", To: "货位"},
			{Name: "引发移动", Label: "triggersMovement", From: "出入库单", To: "库存记录"},
		})},
	{ID: "course-learning", Name: "课程与学习", Description: "课程/章节/学习者/学习记录/掌握度", Source: "教育技术域模式（LMS）", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "课程", Label: "Course", Definition: "结构化的教学内容单元"},
			{Name: "章节", Label: "Chapter", Definition: "课程的组成小节", Parents: []string{"课程"}},
			{Name: "学习者", Label: "Learner", Definition: "参与学习的用户"},
			{Name: "学习记录", Label: "LearningRecord", Definition: "学习者对章节的一次学习事实"},
		},
		[]pkgspec.Relation{
			{Name: "包含章节", Label: "hasChapter", From: "课程", To: "章节"},
			{Name: "学习", Label: "studies", From: "学习者", To: "课程"},
			{Name: "产出记录", Label: "producesRecord", From: "学习者", To: "学习记录"},
		})},
	{ID: "location-address", Name: "位置与地址", Description: "地址/行政层级/地理坐标——位置信息的通用骨架", Source: "schema.org Place 精简对照", Spec: specFragment(
		[]pkgspec.Concept{
			{Name: "地址", Label: "Address", Definition: "结构化的通信地址"},
			{Name: "行政区划", Label: "AdministrativeArea", Definition: "国/省/市/区县层级的管辖区域"},
			{Name: "地理坐标", Label: "GeoCoordinate", Definition: "经纬度定位"},
		},
		[]pkgspec.Relation{
			{Name: "属于行政区", Label: "inArea", From: "地址", To: "行政区划"},
			{Name: "上级行政区", Label: "upperArea", From: "行政区划", To: "行政区划"},
			{Name: "具有坐标", Label: "hasCoordinate", From: "地址", To: "地理坐标"},
		})},
}

// specFragment 构造 ODP 片段 spec（概念/关系；标签/定义齐备以过质量门禁完备性）
func specFragment(concepts []pkgspec.Concept, relations []pkgspec.Relation) *pkgspec.Spec {
	return &pkgspec.Spec{Name: "", Concepts: concepts, Relations: relations, Instances: []pkgspec.Instance{}}
}

// List 返回精选 ODP 清单
func List() []*ODP { return curated }

// Get 按 id 取 ODP
func Get(id string) *ODP {
	for _, o := range curated {
		if o.ID == id {
			return o
		}
	}
	return nil
}
