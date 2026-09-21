package app

import (
	"maps"

	"github.com/uvwt/agentdock/internal/agentinstructions"
	"github.com/uvwt/agentdock/internal/config"
)

type contextRequest struct {
	Workdir string `json:"workdir,omitempty"`
}

func contextToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "agentdock_context", Contract: contextToolContract, Title: "AgentDock context",
		Description: "Return structured AgentDock bootstrap context including capabilities, integrations, rules, and automatically loaded global/workspace AGENTS.md content. Call before project operations; pass workdir when selecting another workspace or refreshing changed rules. Selection is request-local and never changes command defaults.",
		Handler:     ctxToolHandler((*Runtime).agentDockContextTool),
	}}
}

// The standalone entrypoint adds optional local fields without changing the
// shared Nexus Bridge contract. All existing canonical fields remain identical.
func contextToolContract(name string, cfg config.Config) (ToolContract, bool) {
	contract, ok := canonicalToolContract(name, cfg)
	if !ok {
		return ToolContract{}, false
	}
	contract.InputSchema = maps.Clone(contract.InputSchema)
	input := maps.Clone(contract.InputSchema["properties"].(map[string]any))
	input["workdir"] = map[string]any{
		"type": "string", "maxLength": 4096,
		"description": "Existing host workspace directory. Omit or use an empty string for the current default; relative and ~/ paths use Host resolution. Does not change any session or command working directory.",
	}
	contract.InputSchema["properties"] = input
	contract.OutputSchema = maps.Clone(contract.OutputSchema)
	output := maps.Clone(contract.OutputSchema["properties"].(map[string]any))
	extendContextRuntimeSchema(output)
	output["instruction_files"] = instructionFilesSchema()
	output["plugins"] = pluginIndexSchema()
	output["tasks"] = taskIndexSchema()
	output["workspace"] = map[string]any{"type": "object", "additionalProperties": true, "required": []string{"workspace_id", "root", "runtime", "rules_revision"}}
	contract.OutputSchema["properties"] = output
	return contract, true
}

func extendContextRuntimeSchema(properties map[string]any) {
	runtimeSchema, _ := properties["runtime"].(map[string]any)
	if runtimeSchema == nil {
		return
	}
	runtimeCopy := maps.Clone(runtimeSchema)
	runtimeProperties := maps.Clone(runtimeSchema["properties"].(map[string]any))
	runtimeProperties["execution_epoch"] = map[string]any{
		"type": "string", "pattern": "^[a-f0-9]{32}$",
		"description": "Epoch of this running AgentDock process. It scopes execution_request_id and is not a device identity.",
	}
	runtimeProperties["command_recovery"] = map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"version", "request_id_field", "deduplication_scope", "peek", "durable"},
		"properties": map[string]any{
			"version":             map[string]any{"type": "integer"},
			"request_id_field":    map[string]any{"type": "string"},
			"deduplication_scope": map[string]any{"type": "string"},
			"peek":                map[string]any{"type": "boolean"},
			"durable":             map[string]any{"type": "boolean"},
		},
		"description": "In-memory command response recovery. durable=false means a restart does not keep claims.",
	}
	required, _ := runtimeSchema["required"].([]string)
	runtimeCopy["required"] = append(append([]string{}, required...), "execution_epoch", "command_recovery")
	runtimeCopy["properties"] = runtimeProperties
	properties["runtime"] = runtimeCopy
}

func instructionFilesSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"auto_load", "workdir", "workspace_root", "files"},
		"properties": map[string]any{
			"auto_load":      map[string]any{"type": "boolean"},
			"workdir":        map[string]any{"type": "string"},
			"workspace_root": map[string]any{"type": "string"},
			"files": map[string]any{
				"type": "array", "maxItems": agentinstructions.MaxDirectories + 1,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"required": []string{"scope", "path", "status"},
					"properties": map[string]any{
						"scope":        map[string]any{"type": "string", "enum": []string{"global", "workspace"}},
						"path":         map[string]any{"type": "string"},
						"status":       map[string]any{"type": "string", "enum": []string{"loaded", "not_found", "empty", "duplicate", "skipped", "error"}},
						"content":      map[string]any{"type": "string", "maxLength": agentinstructions.MaxFileBytes},
						"sha256":       map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
						"size_bytes":   map[string]any{"type": "integer", "minimum": 0},
						"reason":       map[string]any{"type": "string"},
						"duplicate_of": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}
