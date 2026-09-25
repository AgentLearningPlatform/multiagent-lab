package seed

// REQ-153 大型种子本体（第 6/7 示例）交付测试：嵌入 FS 可列出、可加载、过校验，
// 两个大型种子达到 NFR-O-3「100+ 概念」规模。零外部依赖。

import (
	"testing"
)

func TestLearningExamplesContainNewSeeds(t *testing.T) {
	want := map[string]bool{"med_common": false, "gene_core": false}
	for _, e := range LearningExamples() {
		if _, ok := want[e.Key]; ok {
			want[e.Key] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Fatalf("学习示例列表缺少 %s", k)
		}
	}
}

func TestLoadLearningExampleLargeSeeds(t *testing.T) {
	for _, key := range []string{"med_common", "gene_core"} {
		sp, err := LoadLearningExample(key)
		if err != nil {
			t.Fatalf("加载 %s 失败: %v", key, err)
		}
		if sp.ID == "" || sp.Name == "" {
			t.Fatalf("%s 缺 id/name", key)
		}
		if len(sp.Concepts) < 100 {
			t.Fatalf("%s 概念数 %d 未达 NFR-O-3 百级要求", key, len(sp.Concepts))
		}
		if len(sp.Instances) == 0 || len(sp.Relations) == 0 {
			t.Fatalf("%s 缺实例或关系定义", key)
		}
	}
}

func TestLoadLearningExampleRejectsBadKey(t *testing.T) {
	for _, k := range []string{"", "../secret", "a/b", "x.json"} {
		if _, err := LoadLearningExample(k); err == nil {
			t.Fatalf("key %q 应被拒绝", k)
		}
	}
}
