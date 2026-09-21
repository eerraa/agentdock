package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/activity"
	"github.com/uvwt/agentdock/internal/config"
	coremcp "github.com/uvwt/agentdock/internal/mcp"
)

type droppingTransport struct {
	base    http.RoundTripper
	drop    func(body []byte) bool
	dropped atomic.Int32
}

func (t *droppingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil && request.Body != http.NoBody {
		var err error
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if t.drop != nil && t.drop(body) {
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		t.dropped.Add(1)
		return nil, errors.New("fixture dropped the completed MCP response")
	}
	return response, nil
}

func newReceiptHTTPSession(t *testing.T, transport http.RoundTripper) (*executionHTTPFixture, *sdk.ClientSession) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"), AuthToken: "execution-fixture-token"}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := newHTTPUnrestrictedRuntime(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	serverCore := coremcp.NewServer(runtime, cfg)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", mcpEndpointHandler(serverCore, cfg, nil))
	registerRuntimeAPI(mux, runtime, cfg, nil)
	server := httptest.NewServer(mux)
	if transport == nil {
		transport = executionBearerTransport{base: http.DefaultTransport}
	}
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	agent := sdk.NewClient(&sdk.Implementation{Name: "execution-receipt-fixture", Version: "1"}, nil)
	session, err := agent.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: client, MaxRetries: -1}, nil)
	if err != nil {
		server.Close()
		_ = runtime.Close()
		t.Fatal(err)
	}
	fixture := &executionHTTPFixture{runtime: runtime, server: server, client: client, session: session, root: root}
	t.Cleanup(func() {
		_ = session.Close()
		server.Close()
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	return fixture, session
}

func connectReceiptSession(t *testing.T, fixture *executionHTTPFixture) *sdk.ClientSession {
	t.Helper()
	agent := sdk.NewClient(&sdk.Implementation{Name: "execution-receipt-reconnect", Version: "1"}, nil)
	session, err := agent.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: fixture.server.URL + "/mcp", HTTPClient: fixture.client, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func mcpObject(result *sdk.CallToolResult) (map[string]any, error) {
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func callMCP(session *sdk.ClientSession, ctx context.Context, host, name string, args map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Meta: sdk.Meta{"openai/session": host}, Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	body, err := mcpObject(result)
	if err != nil {
		return nil, err
	}
	if result.IsError {
		return body, errors.New("mcp tool error")
	}
	return body, nil
}

func receiptEpoch(t *testing.T, session *sdk.ClientSession, host string) string {
	t.Helper()
	body, err := callMCP(session, context.Background(), host, "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := body["runtime"].(map[string]any)
	epoch, _ := runtime["execution_epoch"].(string)
	if len(epoch) != 32 {
		t.Fatalf("execution_epoch = %#v", runtime)
	}
	return epoch
}

func newWireRequestID(t *testing.T, epoch string) string {
	t.Helper()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return epoch + "." + hex.EncodeToString(nonce[:])
}

func fileSize(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}

func TestMCPExecReceiptDropsCompletedSyncResponse(t *testing.T) {
	transport := &droppingTransport{base: executionBearerTransport{base: http.DefaultTransport}}
	transport.drop = func(body []byte) bool {
		if !bytes.Contains(body, []byte("exec_command")) {
			return false
		}
		return transport.dropped.Load() == 0
	}
	fixture, session := newReceiptHTTPSession(t, transport)
	host := "receipt-sync-loss"
	epoch := receiptEpoch(t, session, host)
	requestID := newWireRequestID(t, epoch)
	marker := filepath.Join(t.TempDir(), "effects")
	args := map[string]any{"cmd": "printf x >> " + shellQuote(marker) + "; printf x", "execution_mode": "sync", "timeout_ms": 5000, "execution_request_id": requestID}

	if _, err := callMCP(session, context.Background(), host, "exec_command", args); err == nil {
		t.Fatal("completed sync response was delivered to the SDK client")
	}
	if transport.dropped.Load() != 1 || fileSize(t, marker) != 1 {
		t.Fatalf("dropped=%d effects=%d", transport.dropped.Load(), fileSize(t, marker))
	}

	reader := session
	peek, err := callMCP(reader, context.Background(), host, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	if err != nil {
		reader = connectReceiptSession(t, fixture)
		peek, err = callMCP(reader, context.Background(), host, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	}
	receipt, _ := peek["receipt"].(map[string]any)
	if err != nil || peek["stdout"] != "x" || peek["new_execution_started"] != false || peek["call_id"] == receipt["call_id"] || receipt["execution_request_id"] != requestID || fileSize(t, marker) != 1 {
		t.Fatalf("peek after dropped response = %#v %v", peek, err)
	}
	replay, err := callMCP(reader, context.Background(), host, "exec_command", args)
	if err != nil || replay["new_execution_started"] != false || replay["call_id"] != receipt["call_id"] || fileSize(t, marker) != 1 {
		t.Fatalf("replay after dropped response = %#v %v", replay, err)
	}
	if _, err = callMCP(reader, context.Background(), "receipt-sync-other", "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID}); err == nil {
		t.Fatal("another host conversation read the lost execution")
	}
}

func TestMCPExecReceiptCancelLeavesCommandRunning(t *testing.T) {
	fixture, session := newReceiptHTTPSession(t, nil)
	host := "receipt-cancel"
	epoch := receiptEpoch(t, session, host)
	requestID := newWireRequestID(t, epoch)
	marker := filepath.Join(t.TempDir(), "effects")
	args := map[string]any{"cmd": "printf x >> " + shellQuote(marker) + "; sleep 30", "execution_mode": "sync", "timeout_ms": 60000, "execution_request_id": requestID}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := callMCP(session, ctx, host, "exec_command", args)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fileSize(t, marker) == 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("command did not start before cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled SDK call returned the command result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SDK call did not return after cancellation")
	}
	if fileSize(t, marker) != 1 {
		t.Fatalf("cancel changed the process effect: %d", fileSize(t, marker))
	}
	reader := connectReceiptSession(t, fixture)
	peek, err := callMCP(reader, context.Background(), host, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	if err != nil || peek["status"] != "running" || peek["new_execution_started"] != false || fileSize(t, marker) != 1 {
		t.Fatalf("peek after cancel = %#v %v", peek, err)
	}
	replay, err := callMCP(reader, context.Background(), host, "exec_command", args)
	if err != nil || replay["new_execution_started"] != false || replay["call_id"] != peek["receipt"].(map[string]any)["call_id"] || fileSize(t, marker) != 1 {
		t.Fatalf("duplicate after cancel = %#v %v", replay, err)
	}
}

func TestMCPExecReceiptConcurrentCallsStartOnce(t *testing.T) {
	fixture, session := newReceiptHTTPSession(t, nil)
	host := "receipt-http-race"
	epoch := receiptEpoch(t, session, host)
	requestID := newWireRequestID(t, epoch)
	marker := filepath.Join(t.TempDir(), "effects")
	args := map[string]any{"cmd": "printf x >> " + shellQuote(marker) + "; printf x", "execution_mode": "sync", "timeout_ms": 5000, "execution_request_id": requestID}
	const racers = 8
	sessions := make([]*sdk.ClientSession, racers)
	sessions[0] = session
	for i := 1; i < racers; i++ {
		sessions[i] = connectReceiptSession(t, fixture)
	}
	var ready sync.WaitGroup
	var start sync.WaitGroup
	ready.Add(racers)
	start.Add(1)
	results := make([]map[string]any, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ready.Done()
			start.Wait()
			results[index], errs[index] = callMCP(sessions[index], context.Background(), host, "exec_command", args)
		}(i)
	}
	ready.Wait()
	start.Done()
	wg.Wait()
	var callID string
	started := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		id, _ := results[i]["call_id"].(string)
		if id == "" {
			t.Fatalf("racer %d missing call id: %#v", i, results[i])
		}
		if callID == "" {
			callID = id
		}
		if id != callID {
			t.Fatalf("call ids diverged: %s vs %s", id, callID)
		}
		if results[i]["new_execution_started"] == true {
			started++
		}
	}
	if started != 1 || fileSize(t, marker) != 1 {
		t.Fatalf("starts=%d effects=%d", started, fileSize(t, marker))
	}
	page, err := fixture.runtime.ActivityJournal().Calls(context.Background(), activity.CallQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	execs := 0
	for _, call := range page.Calls {
		if call.ToolName == "exec_command" && call.CallID == callID {
			execs++
		}
	}
	if execs != 1 {
		t.Fatalf("exec calls recorded = %d", execs)
	}
}

func shellQuote(path string) string {
	return "'" + path + "'"
}
