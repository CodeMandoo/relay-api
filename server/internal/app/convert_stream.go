package app

import (
	"bufio"
	"encoding/json"
	"strings"
	"time"
)

// This file converts SSE event streams between the OpenAI Chat Completions
// streaming format and the Anthropic Messages streaming format.

type sseEvent struct {
	event string // anthropic-style event name; empty for openai-style streams
	data  string
}

// readSSEEvent reads one blank-line-terminated event block from an SSE stream.
// Lines starting with ":" (comments/heartbeats) are skipped. A trailing event
// without a blank-line terminator is delivered before io.EOF.
func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var ev sseEvent
	var dataLines []string
	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if len(dataLines) > 0 || ev.event != "" {
				ev.data = strings.Join(dataLines, "\n")
				return ev, nil
			}
		} else if strings.HasPrefix(trimmed, ":") {
			// comment / heartbeat, ignore
		} else if strings.HasPrefix(trimmed, "event:") {
			ev.event = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		} else if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimPrefix(trimmed, "data:")
			data = strings.TrimPrefix(data, " ")
			dataLines = append(dataLines, data)
		}
		if err != nil {
			if len(dataLines) > 0 || ev.event != "" {
				ev.data = strings.Join(dataLines, "\n")
				return ev, nil
			}
			return sseEvent{}, err
		}
	}
}

// writeSSEEvent serializes one event in the client-facing protocol.
// Events with a named event-type (Anthropic & Responses) are written with
// "event:" prefix; anonymous events (Chat Completions chunks) use bare "data:".
func writeSSEEvent(to relayProtocol, ev sseEvent) string {
	if ev.event != "" {
		return "event: " + ev.event + "\ndata: " + ev.data + "\n\n"
	}
	return "data: " + ev.data + "\n\n"
}

// streamConverter turns upstream SSE events into client-protocol SSE events.
type streamConverter interface {
	push(ev sseEvent) []sseEvent
	finish() []sseEvent
}

func newStreamConverter(from, to relayProtocol, clientPath string, modelName string) streamConverter {
	if from == relayProtocolAnthropic && to == relayProtocolOpenAI {
		if clientPath == "/v1/responses" {
			return &anthToResponsesStream{model: modelName}
		}
		return &anthToOpenAIStream{model: modelName, toolBlocks: map[int64]int{}}
	}
	if from == relayProtocolOpenAI && to == relayProtocolAnthropic {
		if clientPath == "/v1/responses" {
			return &responsesToAnthStream{model: modelName}
		}
		return &openAIToAnthStream{model: modelName, tcBlocks: map[int64]int64{}}
	}
	return nil
}

func mustJSONEvent(event string, value any) sseEvent {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte("{}")
	}
	return sseEvent{event: event, data: string(data)}
}

func parseEventData(data string) map[string]any {
	var payload map[string]any
	_ = json.Unmarshal([]byte(data), &payload)
	return payload
}

// ---------------------------------------------------------------------------
// Anthropic stream -> OpenAI stream
// ---------------------------------------------------------------------------

type anthToOpenAIStream struct {
	messageID   string
	model       string
	created     int64
	started     bool
	toolBlocks  map[int64]int // anthropic content block index -> openai tool_call index
	toolCount   int
	inputTokens int64
	doneSent    bool
}

func (s *anthToOpenAIStream) createdAt() int64 {
	if s.created == 0 {
		s.created = time.Now().Unix()
	}
	return s.created
}

func (s *anthToOpenAIStream) chunk(delta map[string]any, finishReason any) sseEvent {
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}
	return mustJSONEvent("", map[string]any{
		"id":      firstNonEmpty(s.messageID, "chatcmpl-converted"),
		"object":  "chat.completion.chunk",
		"created": s.createdAt(),
		"model":   s.model,
		"choices": []any{choice},
	})
}

func (s *anthToOpenAIStream) push(ev sseEvent) []sseEvent {
	payload := parseEventData(ev.data)
	switch ev.event {
	case "message_start":
		if message, ok := payload["message"].(map[string]any); ok {
			s.messageID = getString(message, "id")
			if model := getString(message, "model"); model != "" {
				s.model = model
			}
			if usage, ok := message["usage"].(map[string]any); ok {
				s.inputTokens = int64(numberAny(usage["input_tokens"]))
			}
		}
		s.started = true
		return []sseEvent{s.chunk(map[string]any{"role": "assistant"}, nil)}
	case "content_block_start":
		index := int64(numberAny(payload["index"]))
		if block, ok := payload["content_block"].(map[string]any); ok && getString(block, "type") == "tool_use" {
			toolIndex := s.toolCount
			s.toolCount++
			s.toolBlocks[index] = toolIndex
			return []sseEvent{s.chunk(map[string]any{
				"tool_calls": []any{map[string]any{
					"index": toolIndex,
					"id":    getString(block, "id"),
					"type":  "function",
					"function": map[string]any{
						"name":      getString(block, "name"),
						"arguments": "",
					},
				}},
			}, nil)}
		}
		return nil
	case "content_block_delta":
		index := int64(numberAny(payload["index"]))
		delta, _ := payload["delta"].(map[string]any)
		switch getString(delta, "type") {
		case "text_delta":
			return []sseEvent{s.chunk(map[string]any{"content": getString(delta, "text")}, nil)}
		case "input_json_delta":
			if toolIndex, ok := s.toolBlocks[index]; ok {
				return []sseEvent{s.chunk(map[string]any{
					"tool_calls": []any{map[string]any{
						"index":    toolIndex,
						"function": map[string]any{"arguments": getString(delta, "partial_json")},
					}},
				}, nil)}
			}
		}
		return nil
	case "content_block_stop":
		delete(s.toolBlocks, int64(numberAny(payload["index"])))
		return nil
	case "message_delta":
		out := make([]sseEvent, 0, 2)
		stopReason := ""
		if delta, ok := payload["delta"].(map[string]any); ok {
			stopReason = getString(delta, "stop_reason")
		}
		out = append(out, s.chunk(map[string]any{}, anthropicStopToOpenAIFinish(stopReason)))
		outputTokens := int64(0)
		if usage, ok := payload["usage"].(map[string]any); ok {
			outputTokens = int64(numberAny(usage["output_tokens"]))
		}
		out = append(out, mustJSONEvent("", map[string]any{
			"id":      firstNonEmpty(s.messageID, "chatcmpl-converted"),
			"object":  "chat.completion.chunk",
			"created": s.createdAt(),
			"model":   s.model,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":     s.inputTokens,
				"completion_tokens": outputTokens,
				"total_tokens":      s.inputTokens + outputTokens,
			},
		}))
		return out
	case "message_stop":
		s.doneSent = true
		return []sseEvent{{data: "[DONE]"}}
	case "error":
		s.doneSent = true
		return []sseEvent{{data: ev.data}, {data: "[DONE]"}}
	default:
		return nil
	}
}

func (s *anthToOpenAIStream) finish() []sseEvent {
	if s.doneSent {
		return nil
	}
	s.doneSent = true
	return []sseEvent{{data: "[DONE]"}}
}

// ---------------------------------------------------------------------------
// OpenAI stream -> Anthropic stream
// ---------------------------------------------------------------------------

type openAIToAnthStream struct {
	messageID    string
	model        string
	started      bool
	blockOpen    bool
	blockIndex   int64
	nextIndex    int64
	tcBlocks     map[int64]int64 // openai tool_calls index -> anthropic block index
	finished     bool            // message_delta emitted
	stopSent     bool            // message_stop emitted
	inputTokens  int64
	outputTokens int64
}

func (s *openAIToAnthStream) startMessage() sseEvent {
	s.started = true
	return mustJSONEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            firstNonEmpty(s.messageID, "msg_converted"),
			"type":          "message",
			"role":          "assistant",
			"model":         s.model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": s.inputTokens, "output_tokens": 0},
		},
	})
}

func (s *openAIToAnthStream) openTextBlock() sseEvent {
	s.blockOpen = true
	s.blockIndex = s.nextIndex
	s.nextIndex++
	return mustJSONEvent("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         s.blockIndex,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
}

func (s *openAIToAnthStream) closeOpenBlock() []sseEvent {
	if !s.blockOpen {
		return nil
	}
	s.blockOpen = false
	return []sseEvent{mustJSONEvent("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.blockIndex,
	})}
}

func (s *openAIToAnthStream) messageDelta(stopReason string) sseEvent {
	s.finished = true
	return mustJSONEvent("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": s.outputTokens},
	})
}

func (s *openAIToAnthStream) messageStop() sseEvent {
	s.stopSent = true
	return mustJSONEvent("message_stop", map[string]any{"type": "message_stop"})
}

func (s *openAIToAnthStream) push(ev sseEvent) []sseEvent {
	if ev.data == "[DONE]" {
		return s.finish()
	}
	payload := parseEventData(ev.data)
	if payload == nil {
		return nil
	}
	if id := getString(payload, "id"); id != "" {
		s.messageID = id
	}
	if model := getString(payload, "model"); model != "" {
		s.model = model
	}
	out := make([]sseEvent, 0, 4)
	if !s.started {
		out = append(out, s.startMessage())
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		if value := int64(numberAny(usage["prompt_tokens"])); value > 0 {
			s.inputTokens = value
		}
		if value := int64(numberAny(usage["completion_tokens"])); value > 0 {
			s.outputTokens = value
		}
	}
	choices := anySlice(payload["choices"])
	if len(choices) == 0 {
		// Usage-only chunk: if the finish chunk already arrived, close out now.
		if s.finished && !s.stopSent {
			out = append(out, s.messageDelta("end_turn"))
			out = append(out, s.messageStop())
		}
		return out
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)

	if text := getString(delta, "content"); text != "" {
		if !s.blockOpen {
			out = append(out, s.openTextBlock())
		}
		out = append(out, mustJSONEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.blockIndex,
			"delta": map[string]any{"type": "text_delta", "text": text},
		}))
	}

	for _, raw := range anySlice(delta["tool_calls"]) {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		toolIndex := int64(numberAny(call["index"]))
		fn, _ := call["function"].(map[string]any)
		blockIndex, exists := s.tcBlocks[toolIndex]
		if !exists {
			out = append(out, s.closeOpenBlock()...)
			blockIndex = s.nextIndex
			s.nextIndex++
			s.tcBlocks[toolIndex] = blockIndex
			s.blockOpen = true
			s.blockIndex = blockIndex
			out = append(out, mustJSONEvent("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    getString(call, "id"),
					"name":  getString(fn, "name"),
					"input": map[string]any{},
				},
			}))
		}
		if arguments := getString(fn, "arguments"); arguments != "" {
			out = append(out, mustJSONEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": arguments},
			}))
		}
	}

	if reason := getString(choice, "finish_reason"); reason != "" {
		out = append(out, s.closeOpenBlock()...)
		out = append(out, s.messageDelta(openAIFinishToAnthropicStop(reason)))
	}
	return out
}

func (s *openAIToAnthStream) finish() []sseEvent {
	out := make([]sseEvent, 0, 3)
	if !s.started {
		out = append(out, s.startMessage())
	}
	out = append(out, s.closeOpenBlock()...)
	if !s.finished {
		out = append(out, s.messageDelta("end_turn"))
	}
	if !s.stopSent {
		out = append(out, s.messageStop())
	}
	return out
}

// ---------------------------------------------------------------------------
// Anthropic stream -> OpenAI Responses stream
// ---------------------------------------------------------------------------

type anthToResponsesStream struct {
	anthBase    anthToOpenAIStream
	responseID  string
	model       string
	started     bool
	msgAdded    bool
	textIndex   int
	toolIndex   int
	stopReason  string
	accumText   string
	accumToolID string
	doneSent    bool
}

func (s *anthToResponsesStream) responsesEvent(eventType string, data map[string]any) sseEvent {
	return mustJSONEvent("response."+eventType, data)
}

func (s *anthToResponsesStream) push(ev sseEvent) []sseEvent {
	// Delegate to the Chat-chunk converter first.
	chunks := s.anthBase.push(ev)
	out := make([]sseEvent, 0, len(chunks)*2)
	for _, chunk := range chunks {
		out = append(out, s.chatChunkToResponses(chunk)...)
	}
	return out
}

func (s *anthToResponsesStream) chatChunkToResponses(chunk sseEvent) []sseEvent {
	if chunk.data == "[DONE]" {
		return s.finish()
	}
	payload := parseEventData(chunk.data)
	if payload == nil {
		return nil
	}
	out := make([]sseEvent, 0, 3)

	// Forward model/usage info from the inner stream.
	if model := getString(payload, "model"); model != "" {
		s.model = model
	}
	if id := getString(payload, "id"); id != "" && s.responseID == "" {
		s.responseID = id
	}

	choices := anySlice(payload["choices"])
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)

		if !s.started {
			s.started = true
			out = append(out, s.responsesEvent("created", map[string]any{
				"type": "response.created",
				"response": map[string]any{
					"id":     firstNonEmpty(s.responseID, "resp_converted"),
					"object": "response",
					"status": "in_progress",
					"model":  s.model,
				},
			}))
		}

		// On the first event with role=assistant, emit output_item.added for a message.
		if role := getString(delta, "role"); role != "" && !s.msgAdded {
			s.msgAdded = true
			out = append(out, s.responsesEvent("output_item.added", map[string]any{
				"type": "response.output_item.added",
				"item": map[string]any{
					"type":    "message",
					"id":      "msg_" + newRelayRequestID(),
					"role":    "assistant",
					"content": []any{},
				},
			}))
		}

		// tool_calls chunk.
		for _, raw := range anySlice(delta["tool_calls"]) {
			tc, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			tcIndex := int(numberAny(tc["index"]))
			fn, _ := tc["function"].(map[string]any)

			if tcIndex >= s.toolIndex || s.accumToolID == "" {
				// New tool call — close previous text block if open, emit function_call item.
				if s.accumText != "" {
					out = append(out, s.responsesEvent("output_text.done", map[string]any{
						"type":  "response.output_text.done",
						"text":  s.accumText,
						"index": s.textIndex - 1,
					}))
					s.accumText = ""
				}
				s.accumToolID = getString(tc, "id")
				s.toolIndex = tcIndex
				out = append(out, s.responsesEvent("output_item.added", map[string]any{
					"type": "response.output_item.added",
					"item": map[string]any{
						"type":      "function_call",
						"id":        getString(tc, "id"),
						"name":      getString(fn, "name"),
						"arguments": "",
						"status":    "in_progress",
					},
				}))
			}
			if args := getString(fn, "arguments"); args != "" {
				out = append(out, s.responsesEvent("function_call_arguments.delta", map[string]any{
					"type":  "response.function_call_arguments.delta",
					"delta": args,
					"index": tcIndex,
				}))
			}
		}

		// Text content delta.
		if text := getString(delta, "content"); text != "" {
			if s.accumText == "" {
				s.textIndex++
				out = append(out, s.responsesEvent("content_part.added", map[string]any{
					"type":  "response.content_part.added",
					"part":  map[string]any{"type": "text", "text": ""},
					"index": s.textIndex - 1,
				}))
			}
			s.accumText += text
			out = append(out, s.responsesEvent("output_text.delta", map[string]any{
				"type":  "response.output_text.delta",
				"delta": text,
				"index": s.textIndex - 1,
			}))
		}

		// Finish reason — close open items and finalise.
		if reason := getString(choice, "finish_reason"); reason != "" {
			s.stopReason = reason
			if s.accumText != "" {
				out = append(out, s.responsesEvent("output_text.done", map[string]any{
					"type":  "response.output_text.done",
					"text":  s.accumText,
					"index": s.textIndex - 1,
				}))
				s.accumText = ""
			}
			if s.accumToolID != "" {
				out = append(out, s.responsesEvent("function_call_arguments.done", map[string]any{
					"type":      "response.function_call_arguments.done",
					"arguments": "",
					"index":     s.toolIndex,
				}))
				s.accumToolID = ""
			}
		}
	} else if usage, ok := payload["usage"].(map[string]any); ok && s.started {
		// Usage-only chunk — send completed.
		out = append(out, s.finishEvent(usage)...)
	}
	return out
}

func (s *anthToResponsesStream) finishEvent(usage map[string]any) []sseEvent {
	if s.doneSent {
		return nil
	}
	// Mark completed before emitting the terminal event so a later EOF cannot
	// duplicate it, but do not set the flag in finish() before this call.
	s.doneSent = true
	out := make([]sseEvent, 0, 3)
	if s.accumText != "" {
		out = append(out, s.responsesEvent("output_text.done", map[string]any{
			"type":  "response.output_text.done",
			"text":  s.accumText,
			"index": s.textIndex - 1,
		}))
		s.accumText = ""
	}
	if s.accumToolID != "" {
		out = append(out, s.responsesEvent("function_call_arguments.done", map[string]any{
			"type":      "response.function_call_arguments.done",
			"arguments": "",
			"index":     s.toolIndex,
		}))
		s.accumToolID = ""
	}
	out = append(out, s.responsesEvent("completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     firstNonEmpty(s.responseID, "resp_converted"),
			"object": "response",
			"status": "completed",
			"model":  s.model,
			"usage":  usage,
		},
	}))
	return out
}

func (s *anthToResponsesStream) finish() []sseEvent {
	if s.doneSent {
		return nil
	}
	out := s.finishEvent(map[string]any{
		"input_tokens":  s.anthBase.inputTokens,
		"output_tokens": 0,
		"total_tokens":  s.anthBase.inputTokens,
	})
	s.doneSent = true
	return out
}

// ---------------------------------------------------------------------------
// OpenAI Responses stream -> Anthropic stream
// ---------------------------------------------------------------------------

type responsesToAnthStream struct {
	anthBase   openAIToAnthStream
	model      string
	messageID  string
	started    bool
	inputUsage int64
}

func (s *responsesToAnthStream) push(ev sseEvent) []sseEvent {
	payload := parseEventData(ev.data)
	if payload == nil {
		return nil
	}
	eventType := ev.event
	if eventType == "" {
		// Some Responses chunks may omit event type and use bare data.
		eventType = getString(payload, "type")
	}
	out := make([]sseEvent, 0, 4)

	switch {
	case strings.Contains(eventType, "response.created"):
		if response, ok := payload["response"].(map[string]any); ok {
			s.model = getString(response, "model")
			s.messageID = getString(response, "id")
		}
		s.started = true
		return nil // wait for first content

	case strings.Contains(eventType, "output_item.added"):
		if item, ok := payload["item"].(map[string]any); ok {
			if getString(item, "type") == "function_call" {
				name := getString(item, "name")
				id := getString(item, "id")
				if name != "" && id != "" {
					if !s.started {
						out = append(out, s.anthBase.startMessage())
					}
					s.started = true
					s.anthBase.blockOpen = true
					s.anthBase.blockIndex = s.anthBase.nextIndex
					s.anthBase.nextIndex++
					out = append(out, mustJSONEvent("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": s.anthBase.blockIndex,
						"content_block": map[string]any{
							"type":  "tool_use",
							"id":    id,
							"name":  name,
							"input": map[string]any{},
						},
					}))
				}
			}
		}
		return out

	case strings.Contains(eventType, "content_part.added"):
		return nil

	case strings.Contains(eventType, "output_text.delta"):
		if !s.started {
			out = append(out, s.anthBase.startMessage())
			s.started = true
		}
		if !s.anthBase.blockOpen {
			out = append(out, s.anthBase.openTextBlock())
		}
		out = append(out, mustJSONEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.anthBase.blockIndex,
			"delta": map[string]any{"type": "text_delta", "text": getString(payload, "delta")},
		}))
		return out

	case strings.Contains(eventType, "output_text.done"):
		return nil

	case strings.Contains(eventType, "function_call_arguments.delta"):
		if args := getString(payload, "delta"); args != "" {
			out = append(out, mustJSONEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": int64(numberAny(payload["index"])),
				"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
			}))
		}
		return out

	case strings.Contains(eventType, "function_call_arguments.done"):
		out = append(out, closeBlockEvent(int64(numberAny(payload["index"]))))
		return out

	case strings.Contains(eventType, "output_item.done"):
		out = append(out, closeBlockEvent(int64(numberAny(payload["index"]))))
		return out

	case strings.Contains(eventType, "completed"):
		if response, ok := payload["response"].(map[string]any); ok {
			if usage, ok := response["usage"].(map[string]any); ok {
				s.inputUsage = int64(numberAny(usage["input_tokens"]))
			}
		}
		out = append(out, closeBlockEvent(-1))
		out = append(out, mustJSONEvent("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": 0},
		}))
		out = append(out, mustJSONEvent("message_stop", map[string]any{"type": "message_stop"}))
		return out

	default:
		return nil
	}
}

func closeBlockEvent(index int64) sseEvent {
	return mustJSONEvent("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": index,
	})
}

func (s *responsesToAnthStream) finish() []sseEvent {
	return s.anthBase.finish()
}
