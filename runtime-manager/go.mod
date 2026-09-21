module github.com/xiaoyao/eino-multiagent-lab/runtime-manager

go 1.25.0

require (
	github.com/mark3labs/mcp-go v0.43.0
	github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec v0.0.0
)

replace github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec => ../pkg/ontology/spec
