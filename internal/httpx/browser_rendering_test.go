package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProductPagesRealBrowserLocalization(t *testing.T) {
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
	outputRoot = filepath.Join(outputRoot, "http")
	if err := os.MkdirAll(outputRoot, 0700); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{}
	for _, locale := range []string{"ko-KR", "en", "zh-CN"} {
		cfg := testConfig(t)
		cfg.OAuthEnabled = true
		cfg.AuthToken = "fixture-secret-must-not-render"
		cfg.BrowserEnabled = true
		request := httptest.NewRequest(http.MethodGet, "http://fixture.example/", nil)
		request.Header.Set("Accept-Language", locale)
		response := httptest.NewRecorder()
		statusPageHandler(nil, cfg).ServeHTTP(response, request)
		labels := preferredStatusPageText(locale)
		headers := map[string]string{}
		for name, values := range response.Header() {
			if len(values) > 0 {
				headers[name] = values[0]
			}
		}
		cases = append(cases, map[string]any{"name": "status-" + locale, "kind": "page", "locale": locale, "html": response.Body.String(), "headers": headers, "expected": []string{labels.MCPEndpoint, labels.ReadyTitle, labels.Copy, labels.Repository, labels.Documentation}, "absent": []string{cfg.AuthToken}})
		values := url.Values{"response_type": {"code"}, "client_id": {"fixture-client"}, "redirect_uri": {"https://fixture-client.example/callback"}, "code_challenge": {oauthTestChallenge}, "code_challenge_method": {"S256"}, "resource": {"http://fixture.example/mcp"}, "state": {"fixture-state"}}
		response = httptest.NewRecorder()
		writeAuthorizeForm(response, values, "fixture-rejected-password", "Fixture <img src=x onerror=alert(1)>", locale)
		authorize := preferredAuthorizePageText(locale)
		headers = map[string]string{}
		for name, values := range response.Header() {
			if len(values) > 0 {
				headers[name] = values[0]
			}
		}
		cases = append(cases, map[string]any{"name": "oauth-" + locale, "kind": "page", "locale": locale, "html": response.Body.String(), "headers": headers, "expected": []string{authorize.Heading, authorize.PasswordLabel, authorize.Submit, authorize.Cancel, authorize.InvalidPassword, authorize.WarningTitle}, "absent": []string{"fixture-rejected-password"}})
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
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test", "browser-product-renderer.mjs"), fixture, result)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real product-page browser acceptance failed: %v\n%s", err, output)
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
	if !verification.Passed || verification.PassedCases != 8 {
		t.Fatalf("incomplete browser acceptance: %#v", verification)
	}
}
