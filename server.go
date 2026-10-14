package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const maxRequestBytes = 1 << 20

type apiError struct {
	Status  int     `json:"-"`
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

func invalidRequest(message, param, code string) *apiError {
	err := &apiError{Status: http.StatusBadRequest, Message: message, Type: "invalid_request_error", Code: code}
	if param != "" {
		err.Param = &param
	}
	return err
}

func internalError(message, code string) *apiError {
	return &apiError{Status: http.StatusInternalServerError, Message: message, Type: "server_error", Code: code}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err *apiError) {
	writeJSON(w, err.Status, struct {
		Error *apiError `json:"error"`
	}{err})
}

type speechSynthesizer interface {
	Synthesize(context.Context, string, string) ([]byte, *apiError)
}

type server struct {
	cfg    serverConfig
	synth  speechSynthesizer
	logger *log.Logger
}

func newServer(cfg serverConfig, synth speechSynthesizer, logger *log.Logger) *server {
	return &server{cfg: cfg, synth: synth, logger: logger}
}

type statusWriter struct {
	http.ResponseWriter
	status   int
	headOnly bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.headOnly {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	recorded := &statusWriter{ResponseWriter: w, headOnly: r.Method == http.MethodHead}
	w = recorded
	route := r.URL.Path
	var methods []string
	switch route {
	case "/":
		methods = []string{http.MethodGet, http.MethodHead}
	case "/health", "/v1/models":
		methods = []string{http.MethodGet}
	case "/v1/audio/speech":
		methods = []string{http.MethodPost}
	default:
		route = "(unknown)"
	}
	defer func() {
		if s.logger != nil {
			// Do not log arbitrary paths, queries, headers, bodies, or CLI output.
			s.logger.Printf("method=%q route=%s status=%d duration=%s", r.Method, route, recorded.status, time.Since(start).Round(time.Millisecond))
		}
	}()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !validRequestHost(r.Host) {
		writeError(w, &apiError{Status: http.StatusForbidden, Message: "Host must be localhost or a loopback IP address.", Type: "invalid_request_error", Code: "invalid_host"})
		return
	}
	w.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	if origin != "" {
		if !s.cfg.allowsOrigin(origin) {
			writeError(w, &apiError{Status: http.StatusForbidden, Message: "Origin is not allowed. Add a trusted origin with --allowed-origin.", Type: "invalid_request_error", Code: "origin_not_allowed"})
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	if len(methods) == 0 {
		writeError(w, &apiError{Status: http.StatusNotFound, Message: "Endpoint not found.", Type: "invalid_request_error", Code: "not_found"})
		return
	}
	if r.Method == http.MethodOptions {
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")
		requested := r.Header.Get("Access-Control-Request-Method")
		if requested != "" && !slices.Contains(methods, requested) {
			writeError(w, invalidRequest("Requested CORS method is not supported for this endpoint.", "", "invalid_preflight"))
			return
		}
		for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
			switch strings.ToLower(strings.TrimSpace(header)) {
			case "", "content-type", "authorization":
			default:
				writeError(w, invalidRequest("Only Content-Type and Authorization request headers are supported by CORS.", "", "invalid_preflight"))
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ", ")+", OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !slices.Contains(methods, r.Method) {
		w.Header().Set("Allow", strings.Join(methods, ", ")+", OPTIONS")
		writeError(w, &apiError{Status: http.StatusMethodNotAllowed, Message: "Use " + strings.Join(methods, " or ") + " for this endpoint.", Type: "invalid_request_error", Code: "method_not_allowed"})
		return
	}
	switch route {
	case "/":
		writeJSON(w, http.StatusOK, map[string]string{"engine": "aqtpserver", "model": modelID})
	case "/health":
		exists := executableExists(s.cfg.executable)
		status := "ok"
		if !exists {
			status = "degraded"
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": status, "executable_exists": exists})
	case "/v1/models":
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": modelID, "object": "model", "created": 0, "owned_by": "local"},
		}})
	case "/v1/audio/speech":
		s.speech(w, r)
	}
}

type speechRequest struct {
	input string
	voice string
}

func decodeSpeech(body []byte, maxChars int) (speechRequest, *apiError) {
	var request speechRequest
	if !utf8.Valid(body) {
		return request, invalidRequest("Request body must be UTF-8 JSON.", "", "invalid_json")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return request, invalidRequest("Request body must be a JSON object.", "", "invalid_json")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return request, invalidRequest("Request body must contain exactly one JSON object.", "", "invalid_json")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch key {
		case "input", "voice", "model", "response_format", "speed", "instructions":
		default:
			return request, invalidRequest(fmt.Sprintf("Unknown member %q.", key), key, "unknown_parameter")
		}
	}
	for _, key := range []string{"input", "voice", "model", "response_format", "instructions"} {
		raw, exists := fields[key]
		if !exists {
			if key == "input" {
				return request, invalidRequest("input is required.", key, "missing_required_parameter")
			}
			continue
		}
		var value string
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
			return request, invalidRequest(key+" must be a string.", key, "invalid_parameter")
		}
		switch key {
		case "input":
			if strings.TrimSpace(value) == "" {
				return request, invalidRequest("input must not be empty.", key, "empty_input")
			}
			if utf8.RuneCountInString(value) > maxChars {
				return request, invalidRequest(fmt.Sprintf("input exceeds the limit of %d Unicode characters.", maxChars), key, "input_too_long")
			}
			if strings.ContainsRune(value, 0) {
				return request, invalidRequest("input must not contain NUL characters.", key, "invalid_parameter")
			}
			request.input = value
		case "voice":
			if strings.ContainsRune(value, 0) {
				return request, invalidRequest("voice must not contain NUL characters.", key, "invalid_parameter")
			}
			request.voice = value
		case "model":
			if value != modelID {
				return request, invalidRequest("model must be aquestalkplayer or omitted.", key, "unsupported_model")
			}
		case "response_format":
			if value != "wav" {
				return request, invalidRequest("response_format must be wav or omitted.", key, "unsupported_format")
			}
		case "instructions":
			if value != "" {
				return request, invalidRequest("instructions are not supported. Set voice properties in an AquesTalkPlayer GUI preset.", key, "unsupported_parameter")
			}
		}
	}
	if raw, exists := fields["speed"]; exists {
		var speed float64
		if json.Unmarshal(raw, &speed) != nil || speed != 1 {
			return request, invalidRequest("speed must be 1 or omitted. Set speaking speed in an AquesTalkPlayer GUI preset.", "speed", "unsupported_speed")
		}
	}
	return request, nil
}

func (s *server) speech(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.timeout)
	defer cancel()
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, &apiError{Status: http.StatusUnsupportedMediaType, Message: "Content-Type must be application/json.", Type: "invalid_request_error", Code: "unsupported_media_type"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if ctx.Err() != nil {
		writeError(w, contextError(ctx.Err()))
		return
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, &apiError{Status: http.StatusRequestEntityTooLarge, Message: "JSON body exceeds the 1 MiB limit.", Type: "invalid_request_error", Code: "request_too_large"})
		} else {
			writeError(w, invalidRequest("Cannot read the JSON request body.", "", "invalid_json"))
		}
		return
	}
	request, validationErr := decodeSpeech(body, s.cfg.maxInputChars)
	if validationErr != nil {
		writeError(w, validationErr)
		return
	}
	if request.voice == "" {
		request.voice = s.cfg.defaultPreset
	}
	wav, synthesisErr := s.synth.Synthesize(ctx, request.input, request.voice)
	if synthesisErr != nil {
		writeError(w, synthesisErr)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wav)
}
