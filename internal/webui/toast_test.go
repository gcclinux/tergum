package webui

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSetToastTrigger_Success(t *testing.T) {
	w := httptest.NewRecorder()
	setToastTrigger(w, "success", "Backup triggered successfully")

	header := w.Header().Get("HX-Trigger")
	if header == "" {
		t.Fatal("expected HX-Trigger header to be set")
	}

	var payload map[string]toastPayload
	if err := json.Unmarshal([]byte(header), &payload); err != nil {
		t.Fatalf("failed to parse HX-Trigger JSON: %v", err)
	}

	toast, ok := payload["showToast"]
	if !ok {
		t.Fatal("expected 'showToast' key in HX-Trigger payload")
	}
	if toast.Type != "success" {
		t.Errorf("expected type 'success', got %q", toast.Type)
	}
	if toast.Message != "Backup triggered successfully" {
		t.Errorf("expected message 'Backup triggered successfully', got %q", toast.Message)
	}
}

func TestSetToastTrigger_Error(t *testing.T) {
	w := httptest.NewRecorder()
	setToastTrigger(w, "error", "Failed to trigger backup: timeout")

	header := w.Header().Get("HX-Trigger")
	if header == "" {
		t.Fatal("expected HX-Trigger header to be set")
	}

	var payload map[string]toastPayload
	if err := json.Unmarshal([]byte(header), &payload); err != nil {
		t.Fatalf("failed to parse HX-Trigger JSON: %v", err)
	}

	toast := payload["showToast"]
	if toast.Type != "error" {
		t.Errorf("expected type 'error', got %q", toast.Type)
	}
	if toast.Message != "Failed to trigger backup: timeout" {
		t.Errorf("expected message 'Failed to trigger backup: timeout', got %q", toast.Message)
	}
}

func TestSetToastTrigger_AllTypes(t *testing.T) {
	types := []string{"success", "error", "warning", "info"}
	for _, toastType := range types {
		t.Run(toastType, func(t *testing.T) {
			w := httptest.NewRecorder()
			setToastTrigger(w, toastType, "test message")

			header := w.Header().Get("HX-Trigger")
			if header == "" {
				t.Fatal("expected HX-Trigger header to be set")
			}

			var payload map[string]toastPayload
			if err := json.Unmarshal([]byte(header), &payload); err != nil {
				t.Fatalf("failed to parse HX-Trigger JSON: %v", err)
			}

			toast := payload["showToast"]
			if toast.Type != toastType {
				t.Errorf("expected type %q, got %q", toastType, toast.Type)
			}
		})
	}
}

func TestSetSuccessToast(t *testing.T) {
	w := httptest.NewRecorder()
	setSuccessToast(w, "Operation completed")

	header := w.Header().Get("HX-Trigger")
	var payload map[string]toastPayload
	if err := json.Unmarshal([]byte(header), &payload); err != nil {
		t.Fatalf("failed to parse HX-Trigger JSON: %v", err)
	}

	toast := payload["showToast"]
	if toast.Type != "success" {
		t.Errorf("expected type 'success', got %q", toast.Type)
	}
	if toast.Message != "Operation completed" {
		t.Errorf("expected message 'Operation completed', got %q", toast.Message)
	}
}

func TestSetErrorToast(t *testing.T) {
	w := httptest.NewRecorder()
	setErrorToast(w, "Something went wrong")

	header := w.Header().Get("HX-Trigger")
	var payload map[string]toastPayload
	if err := json.Unmarshal([]byte(header), &payload); err != nil {
		t.Fatalf("failed to parse HX-Trigger JSON: %v", err)
	}

	toast := payload["showToast"]
	if toast.Type != "error" {
		t.Errorf("expected type 'error', got %q", toast.Type)
	}
	if toast.Message != "Something went wrong" {
		t.Errorf("expected message 'Something went wrong', got %q", toast.Message)
	}
}

func TestSetToastTrigger_JSONFormat(t *testing.T) {
	w := httptest.NewRecorder()
	setToastTrigger(w, "info", "Hello world")

	header := w.Header().Get("HX-Trigger")

	// Verify it's valid JSON that htmx expects.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(header), &raw); err != nil {
		t.Fatalf("HX-Trigger is not valid JSON: %v", err)
	}

	showToast, ok := raw["showToast"]
	if !ok {
		t.Fatal("missing 'showToast' key in HX-Trigger")
	}

	var detail struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(showToast, &detail); err != nil {
		t.Fatalf("failed to parse showToast detail: %v", err)
	}
	if detail.Type != "info" {
		t.Errorf("expected type 'info', got %q", detail.Type)
	}
	if detail.Message != "Hello world" {
		t.Errorf("expected message 'Hello world', got %q", detail.Message)
	}
}

func TestSetToastTrigger_SpecialCharactersInMessage(t *testing.T) {
	w := httptest.NewRecorder()
	setToastTrigger(w, "error", `Path "C:\Users\test" not found: <nil>`)

	header := w.Header().Get("HX-Trigger")
	if header == "" {
		t.Fatal("expected HX-Trigger header to be set")
	}

	var payload map[string]toastPayload
	if err := json.Unmarshal([]byte(header), &payload); err != nil {
		t.Fatalf("failed to parse HX-Trigger JSON with special characters: %v", err)
	}

	toast := payload["showToast"]
	if toast.Message != `Path "C:\Users\test" not found: <nil>` {
		t.Errorf("message with special characters was mangled: got %q", toast.Message)
	}
}
