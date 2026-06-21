package webui

import (
	"encoding/json"
	"net/http"
)

// toastPayload represents the data sent in the HX-Trigger header to fire
// a showToast event on the client. The client-side Alpine.js listener
// picks this up and displays the toast notification.
type toastPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// TruncateToastMessage truncates a toast message to a maximum of 200 characters.
// Messages at or below 200 characters are returned unchanged.
func TruncateToastMessage(msg string) string {
	if len(msg) > 200 {
		return msg[:200]
	}
	return msg
}

// setToastTrigger sets the HX-Trigger response header with a showToast event
// containing the given toast type and message. The format matches what htmx
// expects for triggering client-side events with detail data:
//
//	{"showToast": {"type": "success", "message": "..."}}
//
// Valid toast types: "success", "error", "warning", "info".
func setToastTrigger(w http.ResponseWriter, toastType, message string) {
	payload := map[string]toastPayload{
		"showToast": {
			Type:    toastType,
			Message: message,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// Should never happen with simple string data, but fail silently
		// rather than breaking the response.
		return
	}
	w.Header().Set("HX-Trigger", string(data))
}

// setSuccessToast is a convenience wrapper for success toasts.
func setSuccessToast(w http.ResponseWriter, message string) {
	setToastTrigger(w, "success", message)
}

// setErrorToast is a convenience wrapper for error toasts.
func setErrorToast(w http.ResponseWriter, message string) {
	setToastTrigger(w, "error", message)
}

// setToastAndEvent sets both a showToast trigger and an additional custom event
// in the HX-Trigger header. This allows a toast notification plus an htmx event
// (e.g., "retentionUpdated") to fire on the client in a single response.
func setToastAndEvent(w http.ResponseWriter, toastType, message, event string) {
	payload := map[string]interface{}{
		"showToast": toastPayload{
			Type:    toastType,
			Message: message,
		},
		event: "",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	w.Header().Set("HX-Trigger", string(data))
}
