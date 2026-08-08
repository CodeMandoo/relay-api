package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// enableProtocolConversion turns on the global protocol conversion switch.
func enableProtocolConversion(t *testing.T, app *App) {
	t.Helper()
	if err := app.db.Model(&PlatformSettings{}).Where("1 = 1").Update("protocol_conversion_enabled", true).Error; err != nil {
		t.Fatalf("enable protocol conversion: %v", err)
	}
}

func TestProtocolConversionOpenAIClientToAnthropicUpstream(t *testing.T) {
	var gotRequest map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-api-key"); got != "anth-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)
		if gotRequest["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"conversion-anth-model\",\"usage\":{\"input_tokens\":9,\"output_tokens\":0}}}\n\n" +
				"event: content_block_start\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"stream hi\"}}\n\n" +
				"event: content_block_stop\n" +
				"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: message_delta\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_1",
			"type":        "message",
			"role":        "assistant",
			"model":       "conversion-anth-model",
			"content":     []any{map[string]any{"type": "text", "text": "hi there"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 9, "output_tokens": 4},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{
		Name:    "Conversion_Anthropic_Source",
		Type:    SourceTypeThirdParty,
		BaseURL: upstream.URL,
		APIKey:  "anth-key",
		Status:  SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "conversion-anth-model", Provider: "Anthropic", Formats: ModelFormatAnthropic, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	key := createRelayAPIKey(t, app)

	// Conversion disabled: no compatible source should be routable.
	disabled := performJSON(app, http.MethodPost, "/v1/chat/completions", key, map[string]any{
		"model":    "conversion-anth-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if disabled.Code == http.StatusOK {
		t.Fatalf("expected failure while conversion is disabled, got 200: %s", disabled.Body.String())
	}

	enableProtocolConversion(t, app)

	resp := performJSON(app, http.MethodPost, "/v1/chat/completions", key, map[string]any{
		"model": "conversion-anth-model",
		"messages": []any{
			map[string]any{"role": "system", "content": "be nice"},
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("converted request failed: %d %s", resp.Code, resp.Body.String())
	}
	body := decodeBody(t, resp)
	choice := body["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "hi there" {
		t.Fatalf("unexpected converted response: %s", resp.Body.String())
	}
	if gotRequest["system"] != "be nice" {
		t.Fatalf("expected system extraction upstream, got %v", gotRequest["system"])
	}
	if gotRequest["max_tokens"].(float64) != 4096 {
		t.Fatalf("expected default max_tokens upstream, got %v", gotRequest["max_tokens"])
	}
	usage := body["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 9 || usage["completion_tokens"].(float64) != 4 {
		t.Fatalf("unexpected usage: %v", usage)
	}

	// Streaming conversion.
	streamResp := performJSON(app, http.MethodPost, "/v1/chat/completions", key, map[string]any{
		"model":    "conversion-anth-model",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if streamResp.Code != http.StatusOK {
		t.Fatalf("converted stream failed: %d %s", streamResp.Code, streamResp.Body.String())
	}
	streamBody := streamResp.Body.String()
	if !strings.Contains(streamBody, "\"content\":\"stream hi\"") {
		t.Fatalf("expected converted content chunk, got:\n%s", streamBody)
	}
	if !strings.Contains(streamBody, "chat.completion.chunk") {
		t.Fatalf("expected openai chunk objects, got:\n%s", streamBody)
	}
	if !strings.Contains(streamBody, "data: [DONE]") {
		t.Fatalf("expected [DONE], got:\n%s", streamBody)
	}
}

func TestProtocolConversionAnthropicClientToOpenAIUpstream(t *testing.T) {
	var gotRequest map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer openai-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)
		if gotRequest["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"conversion-openai-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"model\":\"conversion-openai-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"openai stream\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"model\":\"conversion-openai-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"model\":\"conversion-openai-model\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\n" +
				"data: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-1",
			"object": "chat.completion",
			"model":  "conversion-openai-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "openai says hi"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 2, "total_tokens": 9},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	enableProtocolConversion(t, app)
	source := UpstreamSource{
		Name:    "Conversion_OpenAI_Source",
		Type:    SourceTypeThirdParty,
		BaseURL: upstream.URL,
		APIKey:  "openai-key",
		Status:  SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "conversion-openai-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	key := createRelayAPIKey(t, app)

	resp := performJSON(app, http.MethodPost, "/v1/messages", key, map[string]any{
		"model":      "conversion-openai-model",
		"max_tokens": 64,
		"system":     "be brief",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("converted request failed: %d %s", resp.Code, resp.Body.String())
	}
	body := decodeBody(t, resp)
	if body["type"] != "message" || body["stop_reason"] != "end_turn" {
		t.Fatalf("unexpected converted response: %s", resp.Body.String())
	}
	content := body["content"].([]any)
	if content[0].(map[string]any)["text"] != "openai says hi" {
		t.Fatalf("unexpected content: %s", resp.Body.String())
	}
	messages := gotRequest["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" {
		t.Fatalf("expected system message upstream, got %v", messages[0])
	}
	usage := body["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 7 || usage["output_tokens"].(float64) != 2 {
		t.Fatalf("unexpected usage: %v", usage)
	}

	// Streaming conversion.
	streamResp := performJSON(app, http.MethodPost, "/v1/messages", key, map[string]any{
		"model":      "conversion-openai-model",
		"max_tokens": 64,
		"stream":     true,
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if streamResp.Code != http.StatusOK {
		t.Fatalf("converted stream failed: %d %s", streamResp.Code, streamResp.Body.String())
	}
	streamBody := streamResp.Body.String()
	for _, expected := range []string{"event: message_start", "event: content_block_delta", "openai stream", "\"stop_reason\":\"end_turn\"", "event: message_stop"} {
		if !strings.Contains(streamBody, expected) {
			t.Fatalf("expected %q in converted stream:\n%s", expected, streamBody)
		}
	}
}

func TestProtocolConversionUserModelsCompatibleFormats(t *testing.T) {
	app := testApp(t)
	source := UpstreamSource{
		Name:    "Compatible_Formats_Source",
		Type:    SourceTypeThirdParty,
		BaseURL: "https://api.example.com",
		Status:  SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "compatible-formats-model", Provider: "Anthropic", Formats: ModelFormatAnthropic, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	createTestUser(t, app)
	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)

	findModel := func() map[string]any {
		w := performJSON(app, http.MethodGet, "/api/user/models", userToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("list user models: %d %s", w.Code, w.Body.String())
		}
		body := decodeBody(t, w)
		for _, raw := range body["data"].([]any) {
			item := raw.(map[string]any)
			if item["name"] == "compatible-formats-model" {
				return item
			}
		}
		return nil
	}

	if item := findModel(); item == nil {
		t.Fatalf("expected model in list")
	} else if compatible, ok := item["compatibleFormats"].([]any); ok && len(compatible) > 0 {
		t.Fatalf("expected no compatible formats while disabled, got %v", compatible)
	}

	enableProtocolConversion(t, app)
	item := findModel()
	if item == nil {
		t.Fatalf("expected model in list")
	}
	compatible, _ := item["compatibleFormats"].([]any)
	if len(compatible) != 1 || compatible[0] != ModelFormatOpenAI {
		t.Fatalf("expected openai compatible format, got %v", item["compatibleFormats"])
	}
}

func TestProtocolConversionResponsesClientToAnthropicUpstream(t *testing.T) {
	var gotRequest map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_anth_1",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-3-5-sonnet",
			"content":     []any{map[string]any{"type": "text", "text": "Hello from Claude!"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 7, "output_tokens": 5},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	enableProtocolConversion(t, app)
	source := UpstreamSource{
		Name:    "Responses_To_Anth",
		Type:    SourceTypeThirdParty,
		BaseURL: upstream.URL,
		APIKey:  "anth-key",
		Status:  SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "responses-to-anth-model", Provider: "Anthropic", Formats: ModelFormatAnthropic, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	key := createRelayAPIKey(t, app)

	// Responses API request to an Anthropic upstream.
	resp := performJSON(app, http.MethodPost, "/v1/responses", key, map[string]any{
		"model":        "responses-to-anth-model",
		"instructions": "Be concise.",
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}},
		},
		"max_output_tokens": 256,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("converted response request failed: %d %s", resp.Code, resp.Body.String())
	}
	body := decodeBody(t, resp)
	if body["object"] != "response" || body["status"] != "completed" {
		t.Fatalf("expected responses format, got %v", body)
	}
	output := body["output"].([]any)
	msg := output[0].(map[string]any)
	if msg["type"] != "message" {
		t.Fatalf("expected message output, got %v", output[0])
	}
	// Verify upstream received converted Anthropic format.
	if gotRequest["system"] != "Be concise." {
		t.Fatalf("expected system in anthropic request, got %v", gotRequest["system"])
	}
	messages, _ := gotRequest["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	usage := body["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 7 || usage["output_tokens"].(float64) != 5 {
		t.Fatalf("unexpected usage: %v", usage)
	}
}

func TestProtocolConversionAnthropicClientToOpenAIChatUpstream(t *testing.T) {
	var gotRequest map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-1",
			"object": "chat.completion",
			"model":  "gpt-4o",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "OpenAI response",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	enableProtocolConversion(t, app)
	source := UpstreamSource{
		Name:    "Anth_To_Responses_Source",
		Type:    SourceTypeThirdParty,
		BaseURL: upstream.URL,
		APIKey:  "openai-key",
		Status:  SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	// Model has only openai format (Responses-speaking upstream).
	model := ModelConfig{SourceID: source.ID, Name: "anth-to-responses-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	key := createRelayAPIKey(t, app)

	// Anthropic Messages request routed to an OpenAI upstream via Responses.
	resp := performJSON(app, http.MethodPost, "/v1/messages", key, map[string]any{
		"model":      "anth-to-responses-model",
		"max_tokens": 64,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("converted anthropic request failed: %d %s", resp.Code, resp.Body.String())
	}
	body := decodeBody(t, resp)
	if body["type"] != "message" || body["stop_reason"] != "end_turn" {
		t.Fatalf("expected anthropic format response, got %v", body)
	}
	// Upstream should have received Chat Completions format (the IR).
	messages, _ := gotRequest["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("expected user message upstream, got %v", gotRequest["messages"])
	}
	usage := body["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 3 || usage["output_tokens"].(float64) != 4 {
		t.Fatalf("unexpected usage: %v", usage)
	}
}

func TestProtocolConversionResponsesStreamToAnthropicUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "stream-anth-key" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_s1\",\"model\":\"claude-stream\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
				"event: content_block_start\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"streaming response\"}}\n\n" +
				"event: content_block_stop\n" +
				"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: message_delta\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstream.Close()

	app := testApp(t)
	enableProtocolConversion(t, app)
	source := UpstreamSource{
		Name:    "Responses_Stream_To_Anth",
		Type:    SourceTypeThirdParty,
		BaseURL: upstream.URL,
		APIKey:  "stream-anth-key",
		Status:  SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "responses-stream-anth-model", Provider: "Anthropic", Formats: ModelFormatAnthropic, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	key := createRelayAPIKey(t, app)

	resp := performJSON(app, http.MethodPost, "/v1/responses", key, map[string]any{
		"model":  "responses-stream-anth-model",
		"stream": true,
		"input":  []any{map[string]any{"type": "message", "role": "user", "content": "hi"}},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("converted stream request failed: %d %s", resp.Code, resp.Body.String())
	}
	streamBody := resp.Body.String()
	for _, expected := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		"\"streaming response\"",
		"event: response.completed",
	} {
		if !strings.Contains(streamBody, expected) {
			t.Fatalf("expected %q in responses stream:\n%s", expected, streamBody)
		}
	}
}
