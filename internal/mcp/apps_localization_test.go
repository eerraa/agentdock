package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock-protocol/mcpapps"
)

var localizedAppViews = []string{"agentdock_context", "task_progress", "file_change", "dynamic_mcp", "artifact", "recall", "workflow", "acp_status"}

func TestMCPAppKoreanDictionaryAndRendererContract(t *testing.T) {
	dictionary := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(string(koreanAppMessages)))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatal("invalid Korean dictionary")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		key := token.(string)
		if _, exists := dictionary[key]; exists {
			t.Fatalf("duplicate Korean key %s", key)
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(value) == "" {
			t.Fatalf("empty Korean key %s", key)
		}
		dictionary[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		t.Fatal("trailing dictionary data")
	}
	neutral := mcpapps.HTML("task_progress", "Task")
	start := strings.Index(neutral, "    en:{")
	if start < 0 {
		t.Fatal("pinned dictionary anchor changed")
	}
	end := strings.Index(neutral[start:], "\n    },")
	if end < 0 {
		t.Fatal("pinned dictionary boundary changed")
	}
	fields := regexp.MustCompile(`(\w+):("(?:[^"\\]|\\.)*")`).FindAllStringSubmatch(neutral[start:start+end], -1)
	if len(fields) < 100 {
		t.Fatal("incomplete upstream message inventory")
	}
	placeholders := regexp.MustCompile(`\{\w+\}`)
	for _, field := range fields {
		var english string
		if err := json.Unmarshal([]byte(field[2]), &english); err != nil {
			t.Fatal(err)
		}
		korean, exists := dictionary[field[1]]
		if !exists {
			t.Errorf("Korean translation missing: %s", field[1])
			continue
		}
		before := placeholders.FindAllString(english, -1)
		after := placeholders.FindAllString(korean, -1)
		sort.Strings(before)
		sort.Strings(after)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("placeholder mismatch: %s", field[1])
		}
	}
	for _, view := range localizedAppViews {
		t.Run(view, func(t *testing.T) {
			page := localizedAppHTML(view, "Fixture title")
			if dictionary["title_"+view] == "" {
				t.Fatal("missing localized document title")
			}
			for _, required := range []string{`messages["ko-KR"]`, `candidate==="ko"`, `event.source!==window.parent`, `message.jsonrpc!=="2.0"`, `connect-src 'none'`, `message.params&&message.params.structuredContent`, `refreshLocalePresentation()`, `stateLabel(session.status||state.status)`, `t("role_"+message.role)`} {
				if !strings.Contains(page, required) {
					t.Errorf("missing renderer/security contract %q", required)
				}
			}
			for _, forbidden := range []string{"innerHTML", `rpcRequest("tools/call"`, `rpcRequest("mcp_tool_call"`, `item.append(el("div","message-role",message.role))`} {
				if strings.Contains(page, forbidden) {
					t.Errorf("unsafe or untranslated renderer fragment %q", forbidden)
				}
			}
			rpcPattern := regexp.MustCompile(`rpc(?:Request|Notify)\("[^"]+"`)
			if !reflect.DeepEqual(rpcPattern.FindAllString(page, -1), rpcPattern.FindAllString(mcpapps.HTML(view, "Fixture title"), -1)) {
				t.Fatal("localization changed the MCP App wire calls")
			}
		})
	}
}

// Opt-in browser acceptance requires a real, explicitly selected executable.
// The normal cross-platform suite can skip this environment-specific test;
// the Windows release gate invokes it with AGENTDOCK_ACCEPTANCE_BROWSER set.
func TestMCPAppRealBrowserLocalization(t *testing.T) {
	browser := os.Getenv("AGENTDOCK_ACCEPTANCE_BROWSER")
	if browser == "" {
		t.Skip("real-browser acceptance requires AGENTDOCK_ACCEPTANCE_BROWSER")
	}
	if _, err := os.Stat(browser); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := os.Getenv("AGENTDOCK_BROWSER_TEST_OUTPUT")
	if outputRoot == "" {
		outputRoot = t.TempDir()
	}
	outputRoot = filepath.Join(outputRoot, "mcp")
	if err := os.MkdirAll(outputRoot, 0700); err != nil {
		t.Fatal(err)
	}
	const raw = "fixture original 한국어 中文 <img src=x onerror=alert(1)>"
	outputs := map[string]map[string]any{
		"agentdock_context": {"skills": []any{map[string]any{"name": "fixture-skill", "description": raw}}, "common_skills": map[string]any{"items": []any{map[string]any{"name": "fixture-common", "description": raw}}}, "dynamic_mcp": []any{}, "workflow_templates": []any{}, "recall": map[string]any{"enabled": false}},
		"task_progress":     {"action": "checkpoint", "task": map[string]any{"id": "fixture-task", "title": "fixture-task-title", "status": "in_progress", "summary": raw, "step_count": 2, "completed_step_count": 1, "steps": []any{map[string]any{"id": "first", "title": "fixture-first", "status": "completed"}, map[string]any{"id": "second", "title": "fixture-second", "status": "in_progress"}}}},
		"file_change":       {"action": "replace", "path": `C:\한글 공간\fixture.txt`, "dry_run": true, "changed": true, "insertions": 1, "deletions": 1, "diff_preview": "@@ -1 +1 @@\n-old\n+" + raw},
		"dynamic_mcp":       {"name": "fixture-server:fixture-tool", "result": map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": raw}}}},
		"artifact":          {"filename": "fixture-artifact.txt", "artifact_id": "fixture-artifact-id", "mime_type": "text/plain", "size_bytes": 1234, "sha256": strings.Repeat("a", 64), "expires_at": "2030-01-01T00:00:00Z", "url": "javascript:fixture-must-not-run()"},
		"recall":            {"recall_action": "write", "written": true, "recall_target": "memory", "recall": map[string]any{"title": "fixture-recall", "content": raw, "path": "fixture-recall.md", "frontmatter": map[string]any{"type": "knowledge", "updated_at": "2026-09-23"}}},
		"workflow":          {"action": "match", "count": 1, "best_candidate_score": 0.9, "candidates": []any{map[string]any{"id": "fixture-workflow", "title": "fixture-workflow", "score": 0.9, "reason": raw}}},
		"acp_status":        {"action": "status", "session": map[string]any{"id": "fixture-acp", "status": "running", "agent": "fixture-agent", "cwd": `C:\한글 공간`}, "messages": []any{map[string]any{"role": "user", "content": raw}, map[string]any{"role": "assistant", "content": "fixture-assistant-response"}}},
	}
	korean := map[string][]string{
		"agentdock_context": {"AgentDock Skill", "공통 Skill", "fixture-skill"}, "task_progress": {"진행 기록", "진행 중", "1 / 2단계"},
		"file_change": {"모의 실행", `C:\한글 공간\fixture.txt`}, "dynamic_mcp": {"오류", "외부 MCP", "fixture-server:fixture-tool"},
		"artifact": {"산출물 ID", "만료", "서명된 URL"}, "recall": {"기록", "Recall 업데이트됨"}, "workflow": {"후보 1개", "점수 0.9"}, "acp_status": {"사용자", "어시스턴트", "실행 중"},
	}
	existing := map[string][]string{
		"agentdock_context": {"AgentDock Skills", "Common Skills", "fixture-skill"}, "task_progress": {"CHECKPOINT", "in progress", "1 / 2 steps"},
		"file_change": {"dry run", `C:\한글 공간\fixture.txt`}, "dynamic_mcp": {"error", "external MCP", "fixture-server:fixture-tool"},
		"artifact": {"Artifact ID", "Expires", "Signed URL"}, "recall": {"WRITE", "Recall updated"}, "workflow": {"1 candidates", "score 0.9"}, "acp_status": {"USER", "ASSISTANT", "running"},
	}
	cases := []map[string]any{}
	for _, view := range localizedAppViews {
		for _, locale := range []string{"ko-KR", "en", "zh-CN"} {
			data := outputs[view]
			data["view"] = view
			expected := existing[view]
			waiting := "Waiting for tool output…"
			if locale == "ko-KR" {
				expected = korean[view]
				waiting = "도구 결과를 기다리는 중…"
			}
			item := map[string]any{"name": view + "-" + locale, "kind": "mcp", "locale": locale, "html": localizedAppHTML(view, "Fixture title"), "expected": expected, "waiting": waiting, "output": data}
			if view != "artifact" {
				item["preserved"] = []string{raw}
			}
			cases = append(cases, item)
		}
	}
	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(outputRoot, "fixtures.json")
	result := filepath.Join(outputRoot, "browser-result.json")
	if err := os.WriteFile(fixture, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test", "browser-product-renderer.mjs"), fixture, result)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real browser acceptance failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
	receipt, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var verification struct {
		Passed      bool `json:"passed"`
		PassedCases int  `json:"passed_cases"`
	}
	if err := json.Unmarshal(receipt, &verification); err != nil {
		t.Fatal(err)
	}
	if !verification.Passed || verification.PassedCases != 32 {
		t.Fatalf("incomplete real browser acceptance: %#v", verification)
	}
}
