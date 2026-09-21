module github.com/xiaoyao/eino-multiagent-lab/ontology-service

go 1.25.0

require (
	github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec v0.0.0
	modernc.org/sqlite v1.59.0
)

replace github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec => ../pkg/ontology/spec
