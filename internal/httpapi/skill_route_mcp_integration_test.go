package httpapi

import (
	"context"
	"fmt"
	"testing"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

func TestRouteCarriesOnlyBoundEnabledSkillTools(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()
	remote, _ := newMCPRemote(t)
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	resp, _ := env.do(t, "POST", fmt.Sprintf("/api/mcp/servers/%d/connect", server.ID), teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("connect failed")
	}
	ids := []int64{}
	for _, ns := range []store.NewSkill{
		{ClassID: 1, Name: "Bound", Trigger: "公式", Description: "Bound guidance", Tools: []string{"formula_hint@campus-tools"}, Source: "bound", Enabled: true},
		{ClassID: 1, Name: "Disabled", Trigger: "公式,单位", Description: "Disabled guidance", Tools: []string{"unit_convert@campus-tools"}, Source: "disabled", Enabled: false},
		{ClassID: 1, Name: "Unbound", Trigger: "公式,单位,关系", Description: "Unbound guidance", Tools: []string{"unit_convert@campus-tools"}, Source: "unbound", Enabled: true},
	} {
		sk, err := env.store.CreateSkill(ctx, ns)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sk.ID)
	}
	agent, err := env.store.CreateAgent(ctx, store.NewAgent{ClassID: 1, Name: "Scoped routing", Enabled: true, SkillIDs: ids[:2]})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := env.store.ListAgentCandidateSkills(ctx, 1, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := routeSkill("公式、单位和关系", candidates)
	if got == nil || got.ID != ids[0] || len(got.Tools) != 1 || got.Tools[0] != "formula_hint@campus-tools" {
		t.Fatalf("unbound/disabled tools entered candidates: %+v", got)
	}
}
