package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/activity"
	"github.com/uvwt/agentdock/internal/config"
)

// Use the actual HTTP transport: the SDK client normalizes nil optional params
// to an object and cannot reproduce a raw client omitting the params member.
func TestHTTPDiscoveryOptionalParams(t *testing.T) {
	root := t.TempDir()
	harness := newMCPAppTestHarness(t, config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	})
	server := httptest.NewServer(harness.server.HTTPHandler())
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 10 * time.Second
	for _, method := range []string{"tools/list", "resources/list", "prompts/list"} {
		for _, params := range []struct{ name, member string }{
			{"omitted", ""}, {"null", `,"params":null`}, {"empty", `,"params":{}`},
		} {
			t.Run(method+"/"+params.name, func(t *testing.T) {
				body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q%s}`, method, params.member)
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Accept", "application/json, text/event-stream")
				response, err := client.Do(request)
				if err != nil {
					t.Fatalf("discovery must return a protocol response, not close the connection: %v", err)
				}
				defer response.Body.Close()
				data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
				if err != nil {
					t.Fatal(err)
				}
				var envelope struct {
					JSONRPC string          `json:"jsonrpc"`
					Result  json.RawMessage `json:"result"`
					Error   json.RawMessage `json:"error"`
				}
				if response.StatusCode != http.StatusOK || json.Unmarshal(data, &envelope) != nil || envelope.JSONRPC != "2.0" || len(envelope.Result) == 0 || len(envelope.Error) != 0 {
					t.Fatalf("unexpected discovery response: status=%d body=%s", response.StatusCode, data)
				}
				if method == "tools/list" && !strings.Contains(string(envelope.Result), `"agentdock_context"`) {
					t.Fatal("successful discovery omitted the public bootstrap tool")
				}
			})
		}
	}
}

func TestDiscoveryMetadataAndTypedNilResult(t *testing.T) {
	root := t.TempDir()
	harness := newMCPAppTestHarness(t, config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
	})
	for _, params := range []mcpsdk.Params{nil, (*mcpsdk.ListToolsParams)(nil), (*mcpsdk.ListResourcesParams)(nil), (*mcpsdk.ListPromptsParams)(nil)} {
		if discoveryMeta(params) != nil {
			t.Fatalf("absent optional params exposed metadata: %T", params)
		}
	}
	wantErr := errors.New("discovery fixture failure")
	request := &mcpsdk.ServerRequest[*mcpsdk.ListToolsParams]{
		Params: &mcpsdk.ListToolsParams{Meta: mcpsdk.Meta{"openai/session": "fixture-conversation"}},
	}
	called := false
	handler := harness.server.observeDiscovery(func(ctx context.Context, method string, request mcpsdk.Request) (mcpsdk.Result, error) {
		called = true
		source := activity.SourceFromContext(ctx)
		if source.Provider != "openai" || source.HostConversationID != "fixture-conversation" {
			t.Fatalf("valid explicit conversation metadata was lost: %+v", source)
		}
		return (*mcpsdk.ListToolsResult)(nil), wantErr
	})
	_, err := handler(t.Context(), "tools/list", request)
	if !called || !errors.Is(err, wantErr) {
		t.Fatalf("observation must preserve the handler's failure: called=%v err=%v", called, err)
	}
}
