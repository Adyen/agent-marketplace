package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(1)
		}
		if len(request.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{
				"protocolVersion": request.Params.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "amcompat-local-mcp", "version": "1.0.0"},
			}
		case "ping":
			response["result"] = map[string]any{}
		case "tools/list":
			response["result"] = map[string]any{"tools": []any{map[string]any{"name": "amcompat_echo", "description": "Local compatibility fixture; never calls a model", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "resources/list":
			response["result"] = map[string]any{"resources": []any{}}
		case "resources/templates/list":
			response["result"] = map[string]any{"resourceTemplates": []any{}}
		case "prompts/list":
			response["result"] = map[string]any{"prompts": []any{}}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "Method not supported by inspection-only fixture"}
		}
		if err := encoder.Encode(response); err != nil {
			os.Exit(1)
		}
	}
	if scanner.Err() != nil {
		os.Exit(1)
	}
}
