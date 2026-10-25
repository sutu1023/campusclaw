package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

func mcpSkillSource(name, refs string) string {
	return "名称：" + name + "\n适用条件：勾股,公式\n资料：数学\n工具：" + refs + "\n启用：是\n\n只提示公式适用条件，不直接写出最终得数。\n"
}

func TestSkillLongMCPReferenceFileRoundTrip(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	user, err := env.store.GetUserByUsername(ctx, "teacher_a")
	if err != nil {
		t.Fatal(err)
	}
	name := "campus-" + strings.Repeat("s", 25)
	server, err := env.store.CreateMCPServer(ctx, 1, name, "http://mcp-demo:8090", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	tools, refs := []mcp.Tool{}, []string{}
	for i := 0; i < 8; i++ {
		tool := remoteMCPTools()[0]
		tool.Name = strings.Repeat("t", 63) + fmt.Sprint(i)
		tools = append(tools, tool)
		refs = append(refs, tool.Name+"@"+name)
	}
	operation, err := env.store.BeginMCPConnect(ctx, 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.CompleteMCPDiscovery(ctx, 1, server.ID, operation.Generation, tools, user.ID); err != nil {
		t.Fatal(err)
	}
	source := mcpSkillSource("长引用文件", strings.Join(refs, ","))
	body, _ := json.Marshal(map[string]string{"source": source})
	resp, raw := env.do(t, "POST", "/api/skills", teacher, body, "application/json")
	var created skillJSON
	if resp.StatusCode != 201 || json.Unmarshal(raw, &created) != nil || created.Source != source || len(created.Tools) != 8 || strings.Join(created.Tools, ",") != strings.Join(refs, ",") {
		t.Fatal("long reference file truncated or rejected")
	}
	stored, err := env.store.GetSkill(ctx, 1, created.ID)
	if err != nil || stored.Source != source || len(strings.Join(stored.Tools, ",")) <= 255 {
		t.Fatal("long references not persisted completely")
	}
}

func TestSkillMCPValidationIsAtomicAndOwnershipFirst(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()
	remote, mode := newMCPRemote(t)
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	resp, _ := env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("connect failed")
	}
	source := mcpSkillSource("MCP 解题引导", "knowledge_search,formula_hint@campus-tools")
	body, _ := json.Marshal(map[string]string{"source": source})
	resp, raw := env.do(t, "POST", "/api/skills", teacher, body, "application/json")
	var created skillJSON
	_ = json.Unmarshal(raw, &created)
	if resp.StatusCode != 201 || created.Source != source || len(created.Tools) != 2 {
		t.Fatalf("discovered reference not saved: %d %s", resp.StatusCode, raw)
	}
	unknown, _ := json.Marshal(map[string]string{"source": mcpSkillSource("unknown tool", "missing@campus-tools")})
	resp, raw = env.do(t, "PUT", fmt.Sprintf("/api/skills/%d", created.ID), teacher, unknown, "application/json")
	if resp.StatusCode != 400 || !strings.Contains(string(raw), "missing@campus-tools") {
		t.Fatal("unknown tool error not readable")
	}
	unchanged, err := env.store.GetSkill(ctx, 1, created.ID)
	if err != nil || unchanged.Source != source {
		t.Fatal("failed skill update wrote partial data")
	}
	if _, err := env.store.CreateMCPServer(ctx, 2, "foreign-tools", remote.URL, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"formula_hint@foreign-tools", "formula_hint@absent-tools"} {
		request, _ := json.Marshal(map[string]string{"source": mcpSkillSource("not saved", ref)})
		resp, _ = env.do(t, "POST", "/api/skills", teacher, request, "application/json")
		if resp.StatusCode != 404 {
			t.Fatal("foreign/missing reference not 404")
		}
	}
	foreign, err := env.store.CreateSkill(ctx, store.NewSkill{ClassID: 2, Name: "Foreign skill", Trigger: "x", Description: "body", Source: "source", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = env.do(t, "PUT", fmt.Sprintf("/api/skills/%d", foreign.ID), teacher, []byte(`{"source":"invalid file"}`), "application/json")
	if resp.StatusCode != 404 {
		t.Fatal("foreign skill parsed before ownership")
	}
	resp, _ = env.do(t, "POST", path+"/disconnect", teacher, nil, "")
	if resp.StatusCode != 204 {
		t.Fatal("disconnect failed")
	}
	resp, raw = env.do(t, "PUT", fmt.Sprintf("/api/skills/%d", created.ID), teacher, body, "application/json")
	if resp.StatusCode != 400 || !strings.Contains(string(raw), "服务器") {
		t.Fatal("disconnected reference saved")
	}
	mode.Store(6)
	resp, _ = env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("pending connect failed")
	}
	resp, _ = env.do(t, "PUT", fmt.Sprintf("/api/skills/%d", created.ID), teacher, body, "application/json")
	if resp.StatusCode != 400 {
		t.Fatal("pending reference saved")
	}
	builtin, _ := json.Marshal(map[string]string{"source": mcpSkillSource("Builtin compatible", "knowledge_search")})
	resp, _ = env.do(t, "POST", "/api/skills", teacher, builtin, "application/json")
	if resp.StatusCode != 201 {
		t.Fatal("old builtin skill depends on MCP")
	}
}
