package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This file implements bidirectional conversion between the OpenAI Chat
// Completions format and the Anthropic Messages format. Converters operate on
// generic JSON maps so unknown fields degrade gracefully instead of failing.

// convertRequestBody converts a request payload between relay protocols.
// clientPath identifies the client-facing API route (e.g. "/v1/chat/completions"
// or "/v1/responses") and is used to select the right conversion chain.
func convertRequestBody(from, to relayProtocol, body []byte, clientPath string) ([]byte, error) {
	if from == to {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("invalid request json")
	}
	var out map[string]any
	var err error
	switch {
	case from == relayProtocolOpenAI && to == relayProtocolAnthropic:
		if clientPath == "/v1/responses" {
			out, err = responsesRequestToAnthropic(payload)
		} else {
			out, err = openAIRequestToAnthropic(payload)
		}
	case from == relayProtocolAnthropic && to == relayProtocolOpenAI:
		if clientPath == "/v1/responses" {
			out, err = anthropicRequestToResponses(payload)
		} else {
			out, err = anthropicRequestToOpenAI(payload)
		}
	default:
		return nil, fmt.Errorf("unsupported protocol conversion: %s -> %s", from, to)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// convertResponseBody converts a non-streaming response payload between relay
// protocols. modelName is used when the upstream response omits the model.
// clientPath identifies the client-facing API route.
func convertResponseBody(from, to relayProtocol, body []byte, modelName string, clientPath string) ([]byte, error) {
	if from == to {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("invalid upstream response json")
	}
	var out map[string]any
	switch {
	case from == relayProtocolAnthropic && to == relayProtocolOpenAI:
		if clientPath == "/v1/responses" {
			out = anthropicResponseToResponses(payload, modelName)
		} else {
			out = anthropicResponseToOpenAI(payload, modelName)
		}
	case from == relayProtocolOpenAI && to == relayProtocolAnthropic:
		if clientPath == "/v1/responses" {
			out = openAIResponseToAnthropic(payload, modelName)
		} else {
			out = openAIResponseToAnthropic(payload, modelName)
		}
	default:
		return nil, fmt.Errorf("unsupported protocol conversion: %s -> %s", from, to)
	}
	return json.Marshal(out)
}

// convertErrorBody re-shapes an upstream error payload into the client-facing
// protocol's error envelope, preserving the message when possible.
func convertErrorBody(to relayProtocol, body []byte) []byte {
	message := strings.TrimSpace(string(body))
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if extracted := extractErrorMessage(payload); extracted != "" {
			message = extracted
		}
	}
	if message == "" {
		message = "upstream error"
	}
	var out any
	if to == relayProtocolAnthropic {
		out = map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": message},
		}
	} else {
		out = map[string]any{
			"error": map[string]any{"message": message, "type": "upstream_error", "code": nil},
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return body
	}
	return data
}

func extractErrorMessage(payload map[string]any) string {
	if raw, ok := payload["error"]; ok {
		switch value := raw.(type) {
		case map[string]any:
			if message, ok := value["message"].(string); ok {
				return message
			}
		case string:
			return value
		}
	}
	if message, ok := payload["message"].(string); ok {
		return message
	}
	return ""
}

// convertedUpstreamPath returns the upstream endpoint for a converted request.
func convertedUpstreamPath(to relayProtocol) string {
	if to == relayProtocolAnthropic {
		return "/v1/messages"
	}
	return "/v1/chat/completions"
}

func convertedUpstreamPathForTarget(target routeTarget, to relayProtocol) string {
	if to == relayProtocolOpenAI && normalizeOpenAIProtocol(target.Source.OpenAIProtocol) == "responses" {
		return "/v1/responses"
	}
	return convertedUpstreamPath(to)
}

// ---------------------------------------------------------------------------
// OpenAI request -> Anthropic request
// ---------------------------------------------------------------------------

func openAIRequestToAnthropic(payload map[string]any) (map[string]any, error) {
	out := map[string]any{}
	out["model"] = getString(payload, "model")

	var systemParts []string
	messages := make([]any, 0)
	for _, raw := range anySlice(payload["messages"]) {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := getString(message, "role")
		switch role {
		case "system", "developer":
			if text := openAIContentText(message["content"]); text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": openAIContentToAnthropicBlocks(message["content"]),
			})
		case "assistant":
			messages = append(messages, map[string]any{
				"role":    "assistant",
				"content": openAIAssistantToAnthropicBlocks(message),
			})
		case "tool":
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": getString(message, "tool_call_id"),
				"content":     openAIContentText(message["content"]),
			}
			// Merge consecutive tool results into a single user message.
			if len(messages) > 0 {
				if last, ok := messages[len(messages)-1].(map[string]any); ok && last["role"] == "user" && last["_tool_results"] == true {
					last["content"] = append(anySlice(last["content"]), block)
					continue
				}
			}
			messages = append(messages, map[string]any{"role": "user", "content": []any{block}, "_tool_results": true})
		}
	}
	// Strip internal merge markers.
	for _, raw := range messages {
		if message, ok := raw.(map[string]any); ok {
			delete(message, "_tool_results")
		}
	}
	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n\n")
	}
	out["messages"] = messages

	maxTokens := int64(numberAny(payload["max_tokens"]))
	if maxTokens == 0 {
		maxTokens = int64(numberAny(payload["max_completion_tokens"]))
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	out["max_tokens"] = maxTokens

	copyKeys(payload, out, "temperature", "top_p", "stream")
	if stop, ok := payload["stop"]; ok {
		switch value := stop.(type) {
		case string:
			if value != "" {
				out["stop_sequences"] = []string{value}
			}
		case []any:
			sequences := make([]string, 0, len(value))
			for _, item := range value {
				if text, ok := item.(string); ok && text != "" {
					sequences = append(sequences, text)
				}
			}
			if len(sequences) > 0 {
				out["stop_sequences"] = sequences
			}
		}
	}
	if tools := openAIToolsToAnthropic(payload["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := openAIToolChoiceToAnthropic(payload["tool_choice"]); choice != nil {
		out["tool_choice"] = choice
	}
	if user := getString(payload, "user"); user != "" {
		out["metadata"] = map[string]any{"user_id": user}
	}
	return out, nil
}

// openAIContentText flattens OpenAI message content into plain text.
func openAIContentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if getString(part, "type") == "text" {
				parts = append(parts, getString(part, "text"))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func openAIContentToAnthropicBlocks(content any) []any {
	switch value := content.(type) {
	case string:
		if value == "" {
			return []any{map[string]any{"type": "text", "text": ""}}
		}
		return []any{map[string]any{"type": "text", "text": value}}
	case []any:
		blocks := make([]any, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch getString(part, "type") {
			case "text":
				blocks = append(blocks, map[string]any{"type": "text", "text": getString(part, "text")})
			case "image_url":
				if block := openAIImagePartToAnthropic(part); block != nil {
					blocks = append(blocks, block)
				}
			}
		}
		if len(blocks) == 0 {
			blocks = append(blocks, map[string]any{"type": "text", "text": ""})
		}
		return blocks
	default:
		return []any{map[string]any{"type": "text", "text": ""}}
	}
}

func openAIImagePartToAnthropic(part map[string]any) map[string]any {
	imageURL, _ := part["image_url"].(map[string]any)
	url := getString(imageURL, "url")
	if url == "" {
		return nil
	}
	if strings.HasPrefix(url, "data:") {
		rest := strings.TrimPrefix(url, "data:")
		mediaType := "image/png"
		if index := strings.Index(rest, ";"); index >= 0 {
			mediaType = rest[:index]
			rest = rest[index+1:]
		}
		rest = strings.TrimPrefix(rest, "base64,")
		return map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": mediaType, "data": rest},
		}
	}
	return map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": url},
	}
}

func openAIAssistantToAnthropicBlocks(message map[string]any) []any {
	blocks := make([]any, 0)
	if text := openAIContentText(message["content"]); text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, raw := range anySlice(message["tool_calls"]) {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		input := map[string]any{}
		if arguments := getString(fn, "arguments"); arguments != "" {
			_ = json.Unmarshal([]byte(arguments), &input)
		}
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    getString(call, "id"),
			"name":  getString(fn, "name"),
			"input": input,
		})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, map[string]any{"type": "text", "text": ""})
	}
	return blocks
}

func openAIToolsToAnthropic(tools any) []any {
	out := make([]any, 0)
	for _, raw := range anySlice(tools) {
		tool, ok := raw.(map[string]any)
		if !ok || getString(tool, "type") != "function" {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if fn == nil {
			continue
		}
		schema, _ := fn["parameters"].(map[string]any)
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out = append(out, map[string]any{
			"name":         getString(fn, "name"),
			"description":  getString(fn, "description"),
			"input_schema": schema,
		})
	}
	return out
}

func openAIToolChoiceToAnthropic(choice any) map[string]any {
	switch value := choice.(type) {
	case string:
		switch value {
		case "auto":
			return map[string]any{"type": "auto"}
		case "none":
			return map[string]any{"type": "none"}
		case "required":
			return map[string]any{"type": "any"}
		}
	case map[string]any:
		if getString(value, "type") == "function" {
			fn, _ := value["function"].(map[string]any)
			if name := getString(fn, "name"); name != "" {
				return map[string]any{"type": "tool", "name": name}
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Anthropic request -> OpenAI request
// ---------------------------------------------------------------------------

func anthropicRequestToOpenAI(payload map[string]any) (map[string]any, error) {
	out := map[string]any{}
	out["model"] = getString(payload, "model")

	messages := make([]any, 0)
	if systemText := anthropicSystemText(payload["system"]); systemText != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemText})
	}
	for _, raw := range anySlice(payload["messages"]) {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch getString(message, "role") {
		case "user":
			messages = append(messages, anthropicUserToOpenAIMessages(message)...)
		case "assistant":
			messages = append(messages, anthropicAssistantToOpenAIMessage(message))
		}
	}
	out["messages"] = messages

	copyKeys(payload, out, "max_tokens", "temperature", "top_p", "stream")
	if sequences := stringSlice(payload["stop_sequences"]); len(sequences) > 0 {
		out["stop"] = sequences
	}
	if tools := anthropicToolsToOpenAI(payload["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := anthropicToolChoiceToOpenAI(payload["tool_choice"]); choice != nil {
		out["tool_choice"] = choice
	}
	if metadata, ok := payload["metadata"].(map[string]any); ok {
		if user := getString(metadata, "user_id"); user != "" {
			out["user"] = user
		}
	}
	return out, nil
}

func anthropicSystemText(system any) string {
	switch value := system.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			if block, ok := raw.(map[string]any); ok && getString(block, "type") == "text" {
				parts = append(parts, getString(block, "text"))
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func anthropicUserToOpenAIMessages(message map[string]any) []any {
	content, ok := message["content"].([]any)
	if !ok {
		// String content stays a plain user message.
		return []any{map[string]any{"role": "user", "content": message["content"]}}
	}
	out := make([]any, 0)
	parts := make([]any, 0, len(content))
	flushParts := func() {
		if len(parts) == 0 {
			return
		}
		out = append(out, map[string]any{"role": "user", "content": append([]any(nil), parts...)})
		parts = parts[:0]
	}
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch getString(block, "type") {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": getString(block, "text")})
		case "image":
			if part := anthropicImageBlockToOpenAI(block); part != nil {
				parts = append(parts, part)
			}
		case "tool_result":
			// Tool results become standalone tool-role messages.
			flushParts()
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": getString(block, "tool_use_id"),
				"content":      anthropicToolResultText(block["content"]),
			})
		}
	}
	flushParts()
	if len(out) == 0 {
		out = append(out, map[string]any{"role": "user", "content": ""})
	}
	return out
}

func anthropicImageBlockToOpenAI(block map[string]any) map[string]any {
	source, _ := block["source"].(map[string]any)
	if source == nil {
		return nil
	}
	var url string
	switch getString(source, "type") {
	case "base64":
		url = "data:" + getString(source, "media_type") + ";base64," + getString(source, "data")
	case "url":
		url = getString(source, "url")
	}
	if url == "" {
		return nil
	}
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
}

func anthropicToolResultText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			if block, ok := raw.(map[string]any); ok && getString(block, "type") == "text" {
				parts = append(parts, getString(block, "text"))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func anthropicAssistantToOpenAIMessage(message map[string]any) map[string]any {
	out := map[string]any{"role": "assistant"}
	texts := make([]string, 0)
	toolCalls := make([]any, 0)
	switch content := message["content"].(type) {
	case string:
		if content != "" {
			texts = append(texts, content)
		}
	case []any:
		for _, raw := range content {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch getString(block, "type") {
			case "text":
				texts = append(texts, getString(block, "text"))
			case "tool_use":
				input := block["input"]
				arguments, err := json.Marshal(input)
				if err != nil {
					arguments = []byte("{}")
				}
				toolCalls = append(toolCalls, map[string]any{
					"id":   getString(block, "id"),
					"type": "function",
					"function": map[string]any{
						"name":      getString(block, "name"),
						"arguments": string(arguments),
					},
				})
			}
		}
	}
	text := strings.Join(texts, "\n")
	if text == "" && len(toolCalls) > 0 {
		out["content"] = nil
	} else {
		out["content"] = text
	}
	if len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	return out
}

func anthropicToolsToOpenAI(tools any) []any {
	out := make([]any, 0)
	for _, raw := range anySlice(tools) {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := getString(tool, "name")
		if name == "" {
			continue
		}
		parameters, _ := tool["input_schema"].(map[string]any)
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": getString(tool, "description"),
				"parameters":  parameters,
			},
		})
	}
	return out
}

func anthropicToolChoiceToOpenAI(choice any) any {
	value, ok := choice.(map[string]any)
	if !ok {
		return nil
	}
	switch getString(value, "type") {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "any":
		return "required"
	case "tool":
		if name := getString(value, "name"); name != "" {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Non-streaming response conversion
// ---------------------------------------------------------------------------

func anthropicResponseToOpenAI(payload map[string]any, modelName string) map[string]any {
	message := map[string]any{"role": "assistant"}
	texts := make([]string, 0)
	toolCalls := make([]any, 0)
	for _, raw := range anySlice(payload["content"]) {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch getString(block, "type") {
		case "text":
			texts = append(texts, getString(block, "text"))
		case "tool_use":
			input := block["input"]
			arguments, err := json.Marshal(input)
			if err != nil {
				arguments = []byte("{}")
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   getString(block, "id"),
				"type": "function",
				"function": map[string]any{
					"name":      getString(block, "name"),
					"arguments": string(arguments),
				},
			})
		}
	}
	text := strings.Join(texts, "")
	if text == "" && len(toolCalls) > 0 {
		message["content"] = nil
	} else {
		message["content"] = text
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	model := getString(payload, "model")
	if model == "" {
		model = modelName
	}
	return map[string]any{
		"id":      firstNonEmpty(getString(payload, "id"), "chatcmpl-converted"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       message,
				"finish_reason": anthropicStopToOpenAIFinish(getString(payload, "stop_reason")),
			},
		},
		"usage": anthropicUsageToOpenAI(payload["usage"]),
	}
}

func openAIResponseToAnthropic(payload map[string]any, modelName string) map[string]any {
	content := make([]any, 0)
	finishReason := ""
	choices := anySlice(payload["choices"])
	if len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			finishReason = getString(choice, "finish_reason")
			if message, ok := choice["message"].(map[string]any); ok {
				if text := openAIContentText(message["content"]); text != "" {
					content = append(content, map[string]any{"type": "text", "text": text})
				}
				for _, raw := range anySlice(message["tool_calls"]) {
					call, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := call["function"].(map[string]any)
					input := map[string]any{}
					if arguments := getString(fn, "arguments"); arguments != "" {
						_ = json.Unmarshal([]byte(arguments), &input)
					}
					content = append(content, map[string]any{
						"type":  "tool_use",
						"id":    getString(call, "id"),
						"name":  getString(fn, "name"),
						"input": input,
					})
				}
			}
		}
	}
	model := getString(payload, "model")
	if model == "" {
		model = modelName
	}
	return map[string]any{
		"id":            firstNonEmpty(getString(payload, "id"), "msg_converted"),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   openAIFinishToAnthropicStop(finishReason),
		"stop_sequence": nil,
		"usage":         openAIUsageToAnthropic(payload["usage"]),
	}
}

func anthropicUsageToOpenAI(usage any) map[string]any {
	raw, _ := usage.(map[string]any)
	prompt := int64(numberAny(raw["input_tokens"]))
	completion := int64(numberAny(raw["output_tokens"]))
	cacheRead := int64(numberAny(raw["cache_read_input_tokens"]))
	cacheWrite := int64(numberAny(raw["cache_creation_input_tokens"]))
	out := map[string]any{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
	}
	if cacheRead > 0 || cacheWrite > 0 {
		details := map[string]any{}
		if cacheRead > 0 {
			details["cached_tokens"] = cacheRead
		}
		if cacheWrite > 0 {
			details["cached_creation_tokens"] = cacheWrite
		}
		out["prompt_tokens_details"] = details
	}
	return out
}

func openAIUsageToAnthropic(usage any) map[string]any {
	raw, _ := usage.(map[string]any)
	out := map[string]any{
		"input_tokens":  int64(numberAny(raw["prompt_tokens"])),
		"output_tokens": int64(numberAny(raw["completion_tokens"])),
	}
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		if cached := int64(numberAny(details["cached_tokens"])); cached > 0 {
			out["cache_read_input_tokens"] = cached
		}
		if created := int64(numberAny(details["cached_creation_tokens"])); created > 0 {
			out["cache_creation_input_tokens"] = created
		}
	}
	return out
}

func openAIFinishToAnthropicStop(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func anthropicStopToOpenAIFinish(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	default:
		return "stop"
	}
}

// ---------------------------------------------------------------------------
// Generic helpers
// ---------------------------------------------------------------------------

func anySlice(value any) []any {
	if slice, ok := value.([]any); ok {
		return slice
	}
	return nil
}

func stringSlice(value any) []string {
	items := anySlice(value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func copyKeys(src, dst map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

// ---------------------------------------------------------------------------
// Responses API ↔ Anthropic (through Chat Completions as intermediate format)
// ---------------------------------------------------------------------------

// responsesRequestToAnthropic converts an OpenAI Responses API request to an
// Anthropic Messages request by going through the Chat Completions IR.
func responsesRequestToAnthropic(payload map[string]any) (map[string]any, error) {
	chat := responsesRequestToChat(payload)
	return openAIRequestToAnthropic(chat)
}

// anthropicResponseToResponses converts an Anthropic Messages response to an
// OpenAI Responses API response by going through the Chat Completions IR.
func anthropicResponseToResponses(payload map[string]any, modelName string) map[string]any {
	chat := anthropicResponseToOpenAI(payload, modelName)
	return chatResponseToResponses(chat)
}

// anthropicRequestToResponses converts an Anthropic Messages request to an
// OpenAI Responses API request by going through the Chat Completions IR.
func anthropicRequestToResponses(payload map[string]any) (map[string]any, error) {
	chat, err := anthropicRequestToOpenAI(payload)
	if err != nil {
		return nil, err
	}
	return chatRequestToResponses(chat), nil
}

// openAIResponseToResponses converts a Chat Completions response to an OpenAI
// Responses API response. Used as the second half of the Anthropic→Chat→Responses chain.
func openAIResponseToResponses(payload map[string]any, modelName string) map[string]any {
	return chatResponseToResponses(payload)
}

// responsesContentToChat maps Responses content parts to Chat-compatible parts.
func responsesContentToChat(content any) any {
	if text, ok := content.(string); ok {
		return text
	}
	parts := anySlice(content)
	if len(parts) == 0 {
		return content
	}
	out := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch getString(part, "type") {
		case "input_text":
			out = append(out, map[string]any{"type": "text", "text": getString(part, "text")})
		case "input_image":
			imageURL := getString(part, "image_url")
			if imageURL == "" {
				imageURL = getString(part, "url")
			}
			if imageURL != "" {
				out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}})
			}
		case "text", "image_url":
			out = append(out, part)
		}
	}
	return out
}

func responsesRequestToChat(payload map[string]any) map[string]any {
	out := map[string]any{}
	out["model"] = getString(payload, "model")

	messages := make([]any, 0)
	if instructions := stringAnyNil(payload["instructions"]); instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	for _, raw := range anySlice(payload["input"]) {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch getString(item, "type") {
		case "message", "":
			role := getString(item, "role")
			messages = append(messages, map[string]any{"role": role, "content": responsesContentToChat(item["content"])})
		case "function_call":
			// Function call in input → assistant with tool_calls.
			fn := map[string]any{"name": getString(item, "function"), "arguments": getString(item, "arguments")}
			messages = append(messages, map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":       getString(item, "call_id"),
					"type":     "function",
					"function": fn,
				}},
				"content": nil,
			})
		case "function_call_output":
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": getString(item, "call_id"),
				"content":      getString(item, "output"),
			})
		case "file_search_call", "web_search_call", "computer_call":
			// Not convertible — pass as user text with a warning marker.
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": "[unconvertible " + getString(item, "type") + " result]",
			})
		}
	}
	out["messages"] = messages

	if maxTokens := int64(numberAny(payload["max_output_tokens"])); maxTokens > 0 {
		out["max_tokens"] = maxTokens
	}
	copyKeys(payload, out, "temperature", "top_p", "stream")
	if tools := openAIToolsToAnthropic(payload["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := openAIToolChoiceToAnthropic(payload["tool_choice"]); choice != nil {
		out["tool_choice"] = choice
	}
	// Responses metadata may carry user_id.
	if meta, ok := payload["metadata"].(map[string]any); ok {
		if user := getString(meta, "user_id"); user != "" {
			out["user"] = user
		}
	}
	return out
}

// chatRequestToResponses converts a Chat Completions request to a Responses API
// request. Used when an Anthropic client targets a Responses-speaking upstream.
func chatRequestToResponses(chat map[string]any) map[string]any {
	out := map[string]any{}
	out["model"] = getString(chat, "model")

	input := make([]any, 0)
	for _, raw := range anySlice(chat["messages"]) {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := getString(msg, "role")
		switch role {
		case "system", "developer":
			// System messages go to top-level instructions in Responses.
			if text := openAIContentText(msg["content"]); text != "" {
				out["instructions"] = text
			}
		case "user":
			input = append(input, map[string]any{
				"type":    "message",
				"role":    "user",
				"content": msg["content"],
			})
		case "assistant":
			msgOut := map[string]any{"type": "message", "role": "assistant", "content": []any{}}
			if text := openAIContentText(msg["content"]); text != "" {
				msgOut["content"] = []any{map[string]any{"type": "text", "text": text}}
			}
			input = append(input, msgOut)
			// Tool calls become separate function_call items.
			for _, rawCall := range anySlice(msg["tool_calls"]) {
				call, ok := rawCall.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := call["function"].(map[string]any)
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   getString(call, "id"),
					"function":  getString(fn, "name"),
					"arguments": getString(fn, "arguments"),
				})
			}
		case "tool":
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": getString(msg, "tool_call_id"),
				"output":  openAIContentText(msg["content"]),
			})
		}
	}
	out["input"] = input

	if maxTokens := int64(numberAny(chat["max_tokens"])); maxTokens > 0 {
		out["max_output_tokens"] = maxTokens
	}
	copyKeys(chat, out, "temperature", "top_p", "stream")
	if tools := anthropicToolsToOpenAI(chat["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := anthropicToolChoiceToOpenAI(chat["tool_choice"]); choice != nil {
		out["tool_choice"] = choice
	}
	if user := getString(chat, "user"); user != "" {
		out["metadata"] = map[string]any{"user_id": user}
	}
	return out
}

// chatResponseToResponses converts a Chat Completions response to a Responses
// API response.
func chatResponseToResponses(chat map[string]any) map[string]any {
	model := getString(chat, "model")
	output := make([]any, 0)

	// When called as the second conversion stage (Anthropic→Chat→Responses), the
	// Chat response may carry usage in the Anthropic format; normalise it.
	usageRaw, _ := chat["usage"].(map[string]any)
	usage := map[string]any{
		"input_tokens":  int64(numberAny(usageRaw["prompt_tokens"])),
		"output_tokens": int64(numberAny(usageRaw["completion_tokens"])),
		"total_tokens":  int64(numberAny(usageRaw["total_tokens"])),
	}

	choices := anySlice(chat["choices"])
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		if message, ok := choice["message"].(map[string]any); ok {
			content := make([]any, 0)
			if text := openAIContentText(message["content"]); text != "" {
				content = append(content, map[string]any{"type": "output_text", "text": text})
			}
			output = append(output, map[string]any{
				"type":    "message",
				"id":      "msg_" + newRelayRequestID(),
				"role":    "assistant",
				"content": content,
			})
			for _, raw := range anySlice(message["tool_calls"]) {
				call, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := call["function"].(map[string]any)
				output = append(output, map[string]any{
					"type":      "function_call",
					"id":        getString(call, "id"),
					"name":      getString(fn, "name"),
					"arguments": getString(fn, "arguments"),
					"status":    "completed",
				})
			}
		}
	}

	return map[string]any{
		"id":     firstNonEmpty(getString(chat, "id"), "resp_converted"),
		"object": "response",
		"status": "completed",
		"model":  model,
		"output": output,
		"usage":  usage,
	}
}

// stringAnyNil returns s as a string, or empty if s is nil or not a string.
func stringAnyNil(s any) string {
	if s == nil {
		return ""
	}
	text, ok := s.(string)
	if !ok {
		return ""
	}
	return text
}
