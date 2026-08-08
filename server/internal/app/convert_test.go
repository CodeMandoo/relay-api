package app

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

func bufioReader(input string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(input))
}

func mustConvertRequest(t *testing.T, from, to relayProtocol, body string) map[string]any {
	t.Helper()
	return mustConvertRequestPath(t, from, to, body, "/v1/chat/completions")
}

func mustConvertRequestPath(t *testing.T, from, to relayProtocol, body, clientPath string) map[string]any {
	t.Helper()
	out, err := convertRequestBody(from, to, []byte(body), clientPath)
	if err != nil {
		t.Fatalf("convertRequestBody failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("converted body is not valid json: %v", err)
	}
	return payload
}

func TestOpenAIRequestToAnthropic(t *testing.T) {
	body := `{
		"model": "claude-3-5-sonnet",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"bj\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "sunny"},
			{"role": "tool", "tool_call_id": "call_2", "content": "rainy"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "d", "parameters": {"type": "object"}}}],
		"tool_choice": "auto",
		"stop": "END",
		"stream": true
	}`
	out := mustConvertRequest(t, relayProtocolOpenAI, relayProtocolAnthropic, body)

	if out["system"] != "You are helpful." {
		t.Fatalf("expected system prompt, got %v", out["system"])
	}
	if out["max_tokens"].(float64) != 4096 {
		t.Fatalf("expected default max_tokens 4096, got %v", out["max_tokens"])
	}
	messages := out["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (user, assistant, merged tool user), got %d", len(messages))
	}
	assistant := messages[1].(map[string]any)
	blocks := assistant["content"].([]any)
	var toolUse map[string]any
	for _, raw := range blocks {
		block := raw.(map[string]any)
		if block["type"] == "tool_use" {
			toolUse = block
		}
	}
	if toolUse == nil || toolUse["name"] != "get_weather" {
		t.Fatalf("expected tool_use block, got %v", blocks)
	}
	input := toolUse["input"].(map[string]any)
	if input["city"] != "bj" {
		t.Fatalf("expected parsed tool input, got %v", input)
	}
	toolResults := messages[2].(map[string]any)["content"].([]any)
	if len(toolResults) != 2 {
		t.Fatalf("expected merged tool results, got %d", len(toolResults))
	}
	if out["stop_sequences"].([]any)[0] != "END" {
		t.Fatalf("expected stop sequences, got %v", out["stop_sequences"])
	}
	tools := out["tools"].([]any)
	if tools[0].(map[string]any)["input_schema"] == nil {
		t.Fatalf("expected input_schema on converted tool")
	}
	if out["stream"] != true {
		t.Fatalf("expected stream passthrough")
	}
}

func TestAnthropicRequestToOpenAI(t *testing.T) {
	body := `{
		"model": "gpt-4o",
		"system": "You are helpful.",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "weather?"},
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "sunny"}
			]},
			{"role": "assistant", "content": [
				{"type": "text", "text": "checking"},
				{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "bj"}}
			]}
		],
		"tools": [{"name": "get_weather", "description": "d", "input_schema": {"type": "object"}}],
		"tool_choice": {"type": "any"},
		"stop_sequences": ["END"]
	}`
	out := mustConvertRequest(t, relayProtocolAnthropic, relayProtocolOpenAI, body)

	messages := out["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" {
		t.Fatalf("expected system message first, got %v", messages[0])
	}
	// user text part, tool message, assistant
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(messages), messages)
	}
	toolMsg := messages[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "toolu_1" {
		t.Fatalf("expected tool role message, got %v", toolMsg)
	}
	assistant := messages[3].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	fn := calls[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "get_weather" || !strings.Contains(fn["arguments"].(string), "bj") {
		t.Fatalf("expected converted tool call, got %v", fn)
	}
	if out["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice required, got %v", out["tool_choice"])
	}
	if out["max_tokens"].(float64) != 1024 {
		t.Fatalf("expected max_tokens passthrough, got %v", out["max_tokens"])
	}
}

func TestAnthropicResponseToOpenAI(t *testing.T) {
	body := `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet",
		"content": [{"type": "text", "text": "hello"}],
		"stop_reason": "max_tokens",
		"usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 4}
	}`
	out, err := convertResponseBody(relayProtocolAnthropic, relayProtocolOpenAI, []byte(body), "claude-3-5-sonnet", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("convertResponseBody failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	choice := payload["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "length" {
		t.Fatalf("expected finish_reason length, got %v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Fatalf("expected content hello, got %v", message["content"])
	}
	usage := payload["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 10 || usage["completion_tokens"].(float64) != 5 {
		t.Fatalf("unexpected usage %v", usage)
	}
	if details := usage["prompt_tokens_details"].(map[string]any); details["cached_tokens"].(float64) != 4 {
		t.Fatalf("expected cached tokens detail, got %v", details)
	}
}

func TestOpenAIResponseToAnthropic(t *testing.T) {
	body := `{
		"id": "chatcmpl-1",
		"object": "chat.completion",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "f", "arguments": "{\"x\":1}"}}
			]},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 3, "completion_tokens": 7, "total_tokens": 10}
	}`
	out, err := convertResponseBody(relayProtocolOpenAI, relayProtocolAnthropic, []byte(body), "gpt-4o", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("convertResponseBody failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload["stop_reason"] != "tool_use" {
		t.Fatalf("expected stop_reason tool_use, got %v", payload["stop_reason"])
	}
	content := payload["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "tool_use" || block["id"] != "call_1" {
		t.Fatalf("expected tool_use block, got %v", block)
	}
	usage := payload["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 3 || usage["output_tokens"].(float64) != 7 {
		t.Fatalf("unexpected usage %v", usage)
	}
}

func TestAnthropicStreamToOpenAI(t *testing.T) {
	input := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":12,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":6}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	converter := newStreamConverter(relayProtocolAnthropic, relayProtocolOpenAI, "/v1/chat/completions", "claude")
	var out strings.Builder
	reader := bufioReader(input)
	for {
		ev, err := readSSEEvent(reader)
		if err == nil || ev.data != "" || ev.event != "" {
			for _, converted := range converter.push(ev) {
				out.WriteString(writeSSEEvent(relayProtocolOpenAI, converted))
			}
		}
		if err != nil {
			break
		}
	}
	for _, converted := range converter.finish() {
		out.WriteString(writeSSEEvent(relayProtocolOpenAI, converted))
	}
	result := out.String()

	if !strings.Contains(result, "\"role\":\"assistant\"") {
		t.Fatalf("expected role chunk, got:\n%s", result)
	}
	if !strings.Contains(result, "\"content\":\"hello\"") {
		t.Fatalf("expected content chunk, got:\n%s", result)
	}
	if !strings.Contains(result, "\"finish_reason\":\"stop\"") {
		t.Fatalf("expected finish reason stop, got:\n%s", result)
	}
	if !strings.Contains(result, "\"prompt_tokens\":12") || !strings.Contains(result, "\"completion_tokens\":6") {
		t.Fatalf("expected usage chunk, got:\n%s", result)
	}
	if !strings.HasSuffix(strings.TrimSpace(result), "data: [DONE]") {
		t.Fatalf("expected [DONE] terminator, got:\n%s", result)
	}
}

func TestOpenAIStreamToAnthropic(t *testing.T) {
	input := "data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"x\\\":1}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4o\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n\n" +
		"data: [DONE]\n\n"

	converter := newStreamConverter(relayProtocolOpenAI, relayProtocolAnthropic, "/v1/chat/completions", "gpt-4o")
	var out strings.Builder
	reader := bufioReader(input)
	for {
		ev, err := readSSEEvent(reader)
		if err == nil || ev.data != "" || ev.event != "" {
			for _, converted := range converter.push(ev) {
				out.WriteString(writeSSEEvent(relayProtocolAnthropic, converted))
			}
		}
		if err != nil {
			break
		}
	}
	for _, converted := range converter.finish() {
		out.WriteString(writeSSEEvent(relayProtocolAnthropic, converted))
	}
	result := out.String()

	for _, expected := range []string{
		"event: message_start",
		"event: content_block_start",
		"\"text_delta\"",
		"\"tool_use\"",
		"\"input_json_delta\"",
		"\"stop_reason\":\"tool_use\"",
		"event: message_stop",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("expected %q in output:\n%s", expected, result)
		}
	}
	if !strings.Contains(result, "\"output_tokens\":4") {
		t.Fatalf("expected usage propagation, got:\n%s", result)
	}
}

func TestConvertErrorBody(t *testing.T) {
	out := convertErrorBody(relayProtocolOpenAI, []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := payload["error"].(map[string]any)
	if errObj["message"] != "Overloaded" {
		t.Fatalf("expected message extraction, got %v", errObj)
	}
}

func TestResponsesRequestToAnthropic(t *testing.T) {
	body := `{
		"model": "claude-3-5-sonnet",
		"instructions": "You are helpful.",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hi"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "text", "text": "hello"}]},
			{"type": "function_call_output", "call_id": "call_1", "output": "sunny"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "parameters": {"type": "object"}}}],
		"max_output_tokens": 512,
		"stream": true
	}`
	out := mustConvertRequestPath(t, relayProtocolOpenAI, relayProtocolAnthropic, body, "/v1/responses")

	if out["system"] != "You are helpful." {
		t.Fatalf("expected system, got %v", out["system"])
	}
	messages := out["messages"].([]any)
	// System → out["system"], messages are: user, assistant, user-with-tool-results.
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages in anthropic format, got %d", len(messages))
	}
	last := messages[2].(map[string]any)
	if last["role"] != "user" {
		t.Fatalf("expected last message role=user (with tool_result block), got %v", last["role"])
	}
	content := last["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "tool_result" || block["tool_use_id"] != "call_1" {
		t.Fatalf("expected tool_result block inside user message, got %v", block)
	}
	if out["max_tokens"].(float64) != 512 {
		t.Fatalf("expected max_tokens 512, got %v", out["max_tokens"])
	}
}

func TestAnthropicResponseToResponses(t *testing.T) {
	body := `{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude",
		"content": [{"type": "text", "text": "hello!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 5, "output_tokens": 3}
	}`
	out, err := convertResponseBody(relayProtocolAnthropic, relayProtocolOpenAI, []byte(body), "claude", "/v1/responses")
	if err != nil {
		t.Fatalf("convertResponseBody failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload["object"] != "response" || payload["status"] != "completed" {
		t.Fatalf("expected response object, got %v", payload)
	}
	output := payload["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(output))
	}
	msg := output[0].(map[string]any)
	if msg["type"] != "message" || msg["role"] != "assistant" {
		t.Fatalf("expected message output, got %v", msg)
	}
	content := msg["content"].([]any)
	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "output_text" || textBlock["text"] != "hello!" {
		t.Fatalf("expected output_text, got %v", textBlock)
	}
	usage := payload["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 5 || usage["output_tokens"].(float64) != 3 {
		t.Fatalf("unexpected usage: %v", usage)
	}
}

func TestAnthropicResponseToResponsesWithToolCalls(t *testing.T) {
	body := `{
		"id": "msg_2", "type": "message", "role": "assistant", "model": "claude",
		"content": [
			{"type": "text", "text": "checking weather"},
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "bj"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 15}
	}`
	out, err := convertResponseBody(relayProtocolAnthropic, relayProtocolOpenAI, []byte(body), "claude", "/v1/responses")
	if err != nil {
		t.Fatalf("convertResponseBody failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	output := payload["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected 2 output items (message + function_call), got %d", len(output))
	}
	msg := output[0].(map[string]any)
	if msg["type"] != "message" {
		t.Fatalf("expected message, got %v", output[0])
	}
	fnCall := output[1].(map[string]any)
	if fnCall["type"] != "function_call" || fnCall["name"] != "get_weather" {
		t.Fatalf("expected function_call, got %v", fnCall)
	}
}

func TestResponsesStreamToAnthropic(t *testing.T) {
	input := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-4o\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\",\"index\":0}\n\n" +
		"event: response.output_text.done\n" +
		"data: {\"type\":\"response.output_text.done\",\"text\":\"Hi\",\"index\":0}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-4o\",\"status\":\"completed\"}}\n\n"

	converter := newStreamConverter(relayProtocolOpenAI, relayProtocolAnthropic, "/v1/responses", "gpt-4o")
	var out strings.Builder
	reader := bufioReader(input)
	for {
		ev, err := readSSEEvent(reader)
		if err == nil || ev.data != "" || ev.event != "" {
			for _, converted := range converter.push(ev) {
				out.WriteString(writeSSEEvent(relayProtocolAnthropic, converted))
			}
		}
		if err != nil {
			break
		}
	}
	for _, converted := range converter.finish() {
		out.WriteString(writeSSEEvent(relayProtocolAnthropic, converted))
	}
	result := out.String()

	for _, expected := range []string{
		"event: message_start",
		"text_delta",
		"message_stop",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("expected %q in output:\n%s", expected, result)
		}
	}
}

func TestAnthropicStreamToResponses(t *testing.T) {
	input := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	converter := newStreamConverter(relayProtocolAnthropic, relayProtocolOpenAI, "/v1/responses", "claude")
	var out strings.Builder
	reader := bufioReader(input)
	for {
		ev, err := readSSEEvent(reader)
		if err == nil || ev.data != "" || ev.event != "" {
			for _, converted := range converter.push(ev) {
				out.WriteString(writeSSEEvent(relayProtocolOpenAI, converted))
			}
		}
		if err != nil {
			break
		}
	}
	for _, converted := range converter.finish() {
		out.WriteString(writeSSEEvent(relayProtocolOpenAI, converted))
	}
	result := out.String()

	for _, expected := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		"event: response.completed",
		"\"delta\":\"hello\"",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("expected %q in output:\n%s", expected, result)
		}
	}
}
