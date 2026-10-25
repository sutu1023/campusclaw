package skill

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestToolReferenceSyntax(t *testing.T) {
	for _, raw := range []string{"knowledge_search", "formula_hint@campus-tools", "unit_convert@server_2", "x@a-", "x@a_b", strings.Repeat("t", 64) + "@" + strings.Repeat("s", 32)} {
		ref, err := ParseToolReference(raw)
		if err != nil || ref.String() != raw {
			t.Fatalf("valid reference rejected: %q %v", raw, err)
		}
	}
	for _, raw := range []string{"", "formula_hint", "Formula_hint@campus-tools", "formula-hint@campus-tools", "hint@Campus-tools", "hint@-server", "hint@_server", "hint@1server", "hint@@server", "hint@server@extra", "@server", "hint@", "hint @server", "hint@ server", "KNOWLEDGE_SEARCH", strings.Repeat("t", 65) + "@server", "hint@" + strings.Repeat("s", 33)} {
		if _, err := ParseToolReference(raw); err == nil {
			t.Fatalf("invalid reference accepted: %q", raw)
		}
	}
}

func TestToolSeparatorsDeduplicationAndLimit(t *testing.T) {
	got, err := parseTools(" knowledge_search，formula_hint@campus-tools、unit_convert@campus-tools;knowledge_search；formula_hint@campus-tools|unit_convert@campus-tools ")
	want := []string{"knowledge_search", "formula_hint@campus-tools", "unit_convert@campus-tools"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("dedup/order changed: %v %v", got, err)
	}
	for _, raw := range []string{"", " ,，、;；| "} {
		got, err := parseTools(raw)
		if err != nil || len(got) != 0 {
			t.Fatal("empty tools rejected")
		}
	}
	refs := []string{}
	for i := 0; i < 8; i++ {
		refs = append(refs, fmt.Sprintf("hint_%d@server", i))
	}
	if got, err := parseTools(strings.Join(refs, ",") + "," + refs[0]); err != nil || len(got) != 8 {
		t.Fatal("eight unique references plus duplicate rejected")
	}
	if _, err := parseTools(strings.Join(refs, ",") + ",ninth@server"); err == nil {
		t.Fatal("ninth unique reference accepted")
	}
	long := strings.Repeat("t", 64) + "@" + strings.Repeat("s", 32)
	source := "名称：长引用技能\n适用条件：数学\n工具：knowledge_search," + long + "\n启用：是\n\n原始引导正文。\n"
	parsed, err := Parse(source)
	if err != nil || parsed.Source != source || parsed.Tools[1] != long {
		t.Fatal("long reference or original source lost")
	}
}
