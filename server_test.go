package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type synthFunc func(context.Context, string, string) ([]byte, *apiError)

func (fn synthFunc) Synthesize(ctx context.Context, input, preset string) ([]byte, *apiError) {
	return fn(ctx, input, preset)
}

func requestJSON(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func checkAPIError(t *testing.T, w *httptest.ResponseRecorder, status int, code, param string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Error) != 4 || body.Error["code"] != code || body.Error["message"] == "" || body.Error["type"] == "" {
		t.Fatalf("wrong error shape: %s", w.Body.String())
	}
	if (param == "" && body.Error["param"] != nil) || (param != "" && body.Error["param"] != param) {
		t.Fatalf("wrong param: %s", w.Body.String())
	}
}

func TestRequestValidation(t *testing.T) {
	cfg := defaultConfig("darwin")
	cfg.maxInputChars = 5
	s := newServer(cfg, synthFunc(func(context.Context, string, string) ([]byte, *apiError) {
		t.Fatal("invalid request reached synthesis")
		return nil, nil
	}), nil)
	for _, tc := range []struct{ body, code, param string }{
		{"", "invalid_json", ""},
		{"null", "invalid_json", ""},
		{"[]", "invalid_json", ""},
		{"123", "invalid_json", ""},
		{"{", "invalid_json", ""},
		{`{"input":"a"} {}`, "invalid_json", ""},
		{`{"input":"a"} trailing`, "invalid_json", ""},
		{"{\"input\":\"\xff\"}", "invalid_json", ""},
		{`{}`, "missing_required_parameter", "input"},
		{`{"input":""}`, "empty_input", "input"},
		{`{"input":" \n\t"}`, "empty_input", "input"},
		{`{"input":null}`, "invalid_parameter", "input"},
		{`{"input":42}`, "invalid_parameter", "input"},
		{`{"input":"123456"}`, "input_too_long", "input"},
		{`{"input":"あいうえおか"}`, "input_too_long", "input"},
		{`{"input":"a\u0000"}`, "invalid_parameter", "input"},
		{`{"input":"a","voice":true}`, "invalid_parameter", "voice"},
		{`{"input":"a","voice":null}`, "invalid_parameter", "voice"},
		{`{"input":"a","voice":"\u0000"}`, "invalid_parameter", "voice"},
		{`{"input":"a","model":"tts-1"}`, "unsupported_model", "model"},
		{`{"input":"a","model":""}`, "unsupported_model", "model"},
		{`{"input":"a","model":null}`, "invalid_parameter", "model"},
		{`{"input":"a","response_format":"mp3"}`, "unsupported_format", "response_format"},
		{`{"input":"a","response_format":""}`, "unsupported_format", "response_format"},
		{`{"input":"a","response_format":null}`, "invalid_parameter", "response_format"},
		{`{"input":"a","speed":0}`, "unsupported_speed", "speed"},
		{`{"input":"a","speed":1.5}`, "unsupported_speed", "speed"},
		{`{"input":"a","speed":"1"}`, "unsupported_speed", "speed"},
		{`{"input":"a","speed":null}`, "unsupported_speed", "speed"},
		{`{"input":"a","speed":1e999}`, "unsupported_speed", "speed"},
		{`{"input":"a","instructions":"cheerful"}`, "unsupported_parameter", "instructions"},
		{`{"input":"a","instructions":" "}`, "unsupported_parameter", "instructions"},
		{`{"input":"a","instructions":null}`, "invalid_parameter", "instructions"},
		{`{"input":"a","extra":true}`, "unknown_parameter", "extra"},
		{`{"Input":"a"}`, "unknown_parameter", "Input"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			w := requestJSON(s, "POST", "/v1/audio/speech", tc.body)
			checkAPIError(t, w, 400, tc.code, tc.param)
			if tc.code == "unknown_parameter" && !strings.Contains(w.Body.String(), tc.param) {
				t.Fatal("unknown member not named")
			}
		})
	}
}

func TestSpeechSuccessAndDefaults(t *testing.T) {
	for _, tc := range []struct{ body, defaultPreset, wantInput, wantPreset string }{
		{`{"input":"あいうえお"}`, "", "あいうえお", ""},
		{`{"input":"text"}`, "slow", "text", "slow"},
		{`{"input":"text","voice":""}`, "slow", "text", "slow"},
		{`{"input":"text","voice":"fast"}`, "slow", "text", "fast"},
		{`{"input":"#>ア。","model":"aquestalkplayer","response_format":"wav","speed":1,"instructions":""}`, "", "#>ア。", ""},
		{`{"input":" a ","speed":1.0}`, "", " a ", ""},
	} {
		t.Run(tc.body+tc.defaultPreset, func(t *testing.T) {
			cfg := defaultConfig("darwin")
			cfg.maxInputChars = 5
			cfg.defaultPreset = tc.defaultPreset
			called := false
			s := newServer(cfg, synthFunc(func(ctx context.Context, input, preset string) ([]byte, *apiError) {
				called = true
				if input != tc.wantInput || preset != tc.wantPreset {
					t.Fatalf("input=%q preset=%q", input, preset)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("request has no deadline")
				}
				return testWAV(), nil
			}), nil)
			w := requestJSON(s, "POST", "/v1/audio/speech", tc.body)
			if w.Code != 200 || w.Header().Get("Content-Type") != "audio/wav" || !bytes.Equal(w.Body.Bytes(), testWAV()) || !called {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMediaTypeAndBodyLimit(t *testing.T) {
	s := newServer(defaultConfig("darwin"), nil, nil)
	for _, mediaType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		r := httptest.NewRequest("POST", "http://localhost/v1/audio/speech", strings.NewReader(`{"input":"a"}`))
		r.Header.Set("Content-Type", mediaType)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		checkAPIError(t, w, 415, "unsupported_media_type", "")
	}
	w := requestJSON(s, "POST", "/v1/audio/speech", strings.Repeat(" ", maxRequestBytes+1))
	checkAPIError(t, w, 413, "request_too_large", "")
}

func TestHostProtectionAndCORS(t *testing.T) {
	cfg := defaultConfig("darwin")
	cfg.allowedOrigins = []string{"https://trusted.example"}
	s := newServer(cfg, nil, nil)
	for _, origin := range []string{"http://localhost", "http://localhost:3000", "http://127.0.0.1:5173", "http://[::1]:3000", "app://", "app://desktop", "https://trusted.example"} {
		r := httptest.NewRequest("OPTIONS", "http://127.0.0.1:20203/v1/audio/speech", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "content-type, Authorization")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != origin || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Errorf("origin %q: %d %s", origin, w.Code, w.Body.String())
		}
	}
	for _, origin := range []string{"http://localhost.evil.test", "http://127.0.0.1.evil.test", "http://localhost@evil.test", "http://evil.test@localhost", "http://localhost/path", "http://localhost?query", "http://localhost#", "http://localhost:99999", "http://localhost:", "null", "file://", "https://other.example", "app://desktop/path"} {
		for _, method := range []string{"GET", "OPTIONS"} {
			r := httptest.NewRequest(method, "http://localhost/health", nil)
			r.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			checkAPIError(t, w, 403, "origin_not_allowed", "")
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("disallowed origin received CORS permission")
			}
		}
	}
	for _, host := range []string{"evil.test", "192.168.1.10:20203", "localhost.evil.test", "0.0.0.0", "[::]"} {
		r := httptest.NewRequest("OPTIONS", "http://localhost/v1/audio/speech", nil)
		r.Host = host
		r.Header.Set("Origin", "http://localhost:3000")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		checkAPIError(t, w, 403, "invalid_host", "")
	}
	for _, header := range []string{"Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		r := httptest.NewRequest("OPTIONS", "http://localhost/v1/audio/speech", nil)
		r.Header.Set("Origin", "http://localhost:3000")
		r.Header.Set(header, "unsupported")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		checkAPIError(t, w, 400, "invalid_preflight", "")
	}
}

func TestModelsHealthAndRouting(t *testing.T) {
	cfg := defaultConfig("darwin")
	cfg.executable, _ = os.Executable()
	s := newServer(cfg, nil, nil)
	w := requestJSON(s, "GET", "/v1/models", "")
	var models struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil || w.Code != 200 || models.Object != "list" || len(models.Data) != 1 || models.Data[0].ID != modelID || models.Data[0].Object != "model" {
		t.Fatalf("models: %s %v", w.Body.String(), err)
	}
	for _, exists := range []bool{true, false} {
		if !exists {
			s.cfg.executable = filepath.Join(t.TempDir(), "missing")
		}
		w = requestJSON(s, "GET", "/health", "")
		var health map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil || health["executable_exists"] != exists || w.Code != 200 {
			t.Fatalf("health: %s %v", w.Body.String(), err)
		}
		if !exists && health["status"] != "degraded" {
			t.Fatal("missing executable not reported")
		}
	}
	for _, path := range []string{"/v1/models", "/health", "/v1/audio/speech"} {
		w = requestJSON(s, "DELETE", path, "")
		checkAPIError(t, w, 405, "method_not_allowed", "")
		if w.Header().Get("Allow") == "" {
			t.Fatal("missing Allow header")
		}
	}
	w = requestJSON(s, "GET", "/missing", "")
	checkAPIError(t, w, 404, "not_found", "")
}

func TestHTTPErrorPropagationAndTimeout(t *testing.T) {
	for _, apiErr := range []*apiError{mapExitCode("darwin", 29), mapExitCode("windows", 2016), executableMissing(), contextError(context.DeadlineExceeded)} {
		s := newServer(defaultConfig("darwin"), synthFunc(func(context.Context, string, string) ([]byte, *apiError) {
			return nil, apiErr
		}), nil)
		param := ""
		if apiErr.Param != nil {
			param = *apiErr.Param
		}
		checkAPIError(t, requestJSON(s, "POST", "/v1/audio/speech", `{"input":"a"}`), apiErr.Status, apiErr.Code, param)
	}
	cfg := defaultConfig("darwin")
	cfg.timeout = 10 * time.Millisecond
	s := newServer(cfg, synthFunc(func(ctx context.Context, _, _ string) ([]byte, *apiError) {
		<-ctx.Done()
		return nil, contextError(ctx.Err())
	}), nil)
	checkAPIError(t, requestJSON(s, "POST", "/v1/audio/speech", `{"input":"a"}`), 504, "synthesis_timeout", "")
}

func TestLogsDoNotContainInput(t *testing.T) {
	var logs bytes.Buffer
	s := newServer(defaultConfig("darwin"), synthFunc(func(context.Context, string, string) ([]byte, *apiError) {
		return testWAV(), nil
	}), log.New(&logs, "", 0))
	requestJSON(s, "POST", "/v1/audio/speech?input=secret-query", `{"input":"secret-input","voice":"secret-preset"}`)
	requestJSON(s, "GET", "/secret-path", "")
	if strings.Contains(logs.String(), "secret") || strings.Count(logs.String(), "\n") != 2 || !strings.Contains(logs.String(), "status=200") {
		t.Fatal(logs.String())
	}
}

func TestHTTPRealExecRoundTrip(t *testing.T) {
	t.Setenv("AQTPSERVER_TEST_HELPER", "1")
	synth := testSynth(t, execRunner{})
	if runtime.GOOS == "windows" {
		synth.platform = "windows"
	}
	cfg := defaultConfig(synth.platform)
	cfg.executable = synth.executable
	ts := httptest.NewServer(newServer(cfg, synth, nil))
	defer ts.Close()
	response, err := ts.Client().Post(ts.URL+"/v1/audio/speech", "application/json", strings.NewReader(`{"input":"こんにちは","speed":1.0}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	wav, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != "audio/wav" || !reflect.DeepEqual(wav, testWAV()) {
		t.Fatalf("response: %d %d bytes %v", response.StatusCode, len(wav), err)
	}
	assertClean(t, synth.tempRoot)
}
