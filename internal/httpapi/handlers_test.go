package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/screening"
)

func testHandler() *Handler {
	svc := screening.NewService()
	return NewHandler(svc, slog.Default(), ServiceInfo{
		Version:      "0.9.0-test",
		ProviderName: "local",
		RecordCount:  3,
		ListVersion:  "sample-v1",
		DataSource:   "data/raw/sdn-sample.json",
	}, nil, nil, nil)
}

// testRouter builds a router in legacy mode (no tenant stores).
func testRouter(h *Handler) http.Handler {
	return NewRouter(h, RouterOptions{Logger: slog.Default()})
}

func errorBody(t *testing.T, body []byte) (code, msg string) {
	t.Helper()
	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	return resp.Error.Code, resp.Error.Message
}

// --- /health ---

func TestHealthEndpoint(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)

	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
	if body["service"] != "sanctions-screening-api" {
		t.Errorf("expected service=sanctions-screening-api, got %q", body["service"])
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /health: expected 405, got %d", method, rec.Code)
		}
		code, msg := errorBody(t, rec.Body.Bytes())
		if code != ErrCodeMethodNotAllowed {
			t.Errorf("%s /health: expected code %q, got %q", method, ErrCodeMethodNotAllowed, code)
		}
		if msg != "method not allowed" {
			t.Errorf("%s /health: expected msg 'method not allowed', got %q", method, msg)
		}
	}
}

// --- /ready ---

func TestReadyEndpoint(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)

	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", body["status"])
	}
	if body["record_count"].(float64) != 3 {
		t.Errorf("expected record_count=3, got %v", body["record_count"])
	}
}

func TestReadyEndpoint_NoData(t *testing.T) {
	svc := screening.NewService()
	h := NewHandler(svc, slog.Default(), ServiceInfo{
		Version:      "0.9.0-test",
		ProviderName: "local",
		RecordCount:  0,
	}, nil, nil, nil)
	router := testRouter(h)

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)

	if body["status"] != "no-data" {
		t.Errorf("expected status=no-data, got %v", body["status"])
	}
}

func TestReadyEndpoint_MethodNotAllowed(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("POST", "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// --- /version ---

func TestVersionEndpoint(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("GET", "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)

	if body["version"] != "0.9.0-test" {
		t.Errorf("expected version=0.9.0-test, got %q", body["version"])
	}
	if body["service"] != "sanctions-screening-api" {
		t.Errorf("expected service=sanctions-screening-api, got %q", body["service"])
	}
}

func TestVersionEndpoint_DefaultDev(t *testing.T) {
	svc := screening.NewService()
	h := NewHandler(svc, slog.Default(), ServiceInfo{
		Version:      "",
		ProviderName: "local",
		RecordCount:  0,
	}, nil, nil, nil)
	router := testRouter(h)

	req := httptest.NewRequest("GET", "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)

	if body["service"] != "sanctions-screening-api" {
		t.Errorf("expected service name, got %q", body["service"])
	}
}

func TestVersionEndpoint_MethodNotAllowed(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("DELETE", "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// --- /metadata ---

func TestMetadataEndpoint(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("GET", "/metadata", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)

	if body["provider"] != "local" {
		t.Errorf("expected provider=local, got %v", body["provider"])
	}
	if body["record_count"].(float64) != 3 {
		t.Errorf("expected record_count=3, got %v", body["record_count"])
	}
	if body["list_version"] != "sample-v1" {
		t.Errorf("expected list_version=sample-v1, got %v", body["list_version"])
	}
	if body["data_source"] != "data/raw/sdn-sample.json" {
		t.Errorf("expected data_source, got %v", body["data_source"])
	}
	if body["version"] != "0.9.0-test" {
		t.Errorf("expected version=0.9.0-test, got %v", body["version"])
	}
}

func TestMetadataEndpoint_MethodNotAllowed(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("PUT", "/metadata", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// --- /screen ---

func TestScreenEndpoint_Success(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe", "entity_type": "individual"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)

	if rid, ok := result["request_id"].(string); !ok || rid == "" {
		t.Error("expected non-empty request_id")
	}
}

func TestScreenEndpoint_MethodNotAllowed(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	for _, method := range []string{"GET", "PUT", "PATCH", "DELETE"} {
		req := httptest.NewRequest(method, "/screen", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /screen: expected 405, got %d", method, rec.Code)
		}
	}
}

func TestScreenEndpoint_InvalidJSON(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	req := httptest.NewRequest("POST", "/screen", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	code, _ := errorBody(t, rec.Body.Bytes())
	if code != ErrCodeInvalidBody {
		t.Errorf("expected code %q, got %q", ErrCodeInvalidBody, code)
	}
}

func TestScreenEndpoint_MissingName(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"entity_type": "individual"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	code, _ := errorBody(t, rec.Body.Bytes())
	if code != ErrCodeMissingField {
		t.Errorf("expected code %q, got %q", ErrCodeMissingField, code)
	}
}

func TestScreenEndpoint_EmptyName(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "", "entity_type": "individual"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestScreenEndpoint_ThresholdOutOfRange(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe", "threshold": 1.5}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	code, _ := errorBody(t, rec.Body.Bytes())
	if code != ErrCodeInvalidThreshold {
		t.Errorf("expected code %q, got %q", ErrCodeInvalidThreshold, code)
	}
}

func TestScreenEndpoint_InvalidEntityType(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe", "entity_type": "corp"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	code, _ := errorBody(t, rec.Body.Bytes())
	if code != ErrCodeInvalidEntityType {
		t.Errorf("expected code %q, got %q", ErrCodeInvalidEntityType, code)
	}
}

func TestScreenEndpoint_UnknownField(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe", "unknown_field": "test"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestScreenEndpoint_TrailingData(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe"}{"query_name": "Jane Doe"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	code, _ := errorBody(t, rec.Body.Bytes())
	if code != ErrCodeTrailingData {
		t.Errorf("expected code %q, got %q", ErrCodeTrailingData, code)
	}
}

func TestScreenEndpoint_EntityTypeCaseInsensitive(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe", "entity_type": "INDIVIDUAL"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCandidatesIsArray(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "John Doe"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)

	candidates, ok := result["candidates"].([]any)
	if !ok {
		t.Fatalf("expected candidates to be an array, got %T", result["candidates"])
	}
	if len(candidates) != 0 {
		t.Errorf("expected empty candidates array, got %d items", len(candidates))
	}
}

// --- API key middleware ---

func TestAPIKeyMiddleware_Disabled(t *testing.T) {
	// No API_KEY set → middleware is pass-through
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "test"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when API_KEY unset, got %d", rec.Code)
	}
}

func TestAPIKeyMiddleware_Enabled_ValidKey(t *testing.T) {
	t.Setenv("API_KEY", "test-secret-key")
	defer os.Unsetenv("API_KEY")

	// Rebuild router with middleware enabled
	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "test"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	req.Header.Set("X-API-Key", "test-secret-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid key, got %d", rec.Code)
	}
}

func TestAPIKeyMiddleware_Enabled_MissingKey(t *testing.T) {
	t.Setenv("API_KEY", "test-secret-key")
	defer os.Unsetenv("API_KEY")

	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "test"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	code, _ := errorBody(t, rec.Body.Bytes())
	if code != ErrCodeUnauthorized {
		t.Errorf("expected code %q, got %q", ErrCodeUnauthorized, code)
	}
}

func TestAPIKeyMiddleware_Enabled_WrongKey(t *testing.T) {
	t.Setenv("API_KEY", "correct-key")
	defer os.Unsetenv("API_KEY")

	h := testHandler()
	router := testRouter(h)

	body := strings.NewReader(`{"query_name": "test"}`)
	req := httptest.NewRequest("POST", "/screen", body)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- Error shape consistency ---

func TestErrorShape_HasCodeAndMessage(t *testing.T) {
	h := testHandler()
	router := testRouter(h)

	// Trigger any error
	req := httptest.NewRequest("POST", "/screen", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if body.Error.Code == "" {
		t.Error("expected non-empty error.code")
	}
	if body.Error.Message == "" {
		t.Error("expected non-empty error.message")
	}
}
