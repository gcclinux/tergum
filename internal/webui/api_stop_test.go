package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockBackupTrigger struct {
	stopCalled bool
	stopError  error
}

func (m *mockBackupTrigger) TriggerBackup(level string) error {
	return nil
}

func (m *mockBackupTrigger) StopBackup() error {
	m.stopCalled = true
	return m.stopError
}

func (m *mockBackupTrigger) IsAvailable() bool {
	return true
}

func TestHandleAPIBackupStop(t *testing.T) {
	t.Run("nil trigger", func(t *testing.T) {
		s := &Server{
			backupTrigger: nil,
		}
		req := httptest.NewRequest(http.MethodPost, "/api/backups/stop", nil)
		w := httptest.NewRecorder()
		s.handleAPIBackupStop(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Backup trigger not available") {
			t.Errorf("unexpected body: %s", w.Body.String())
		}
	})

	t.Run("stop success", func(t *testing.T) {
		trigger := &mockBackupTrigger{}
		s := &Server{
			backupTrigger: trigger,
		}
		req := httptest.NewRequest(http.MethodPost, "/api/backups/stop", nil)
		w := httptest.NewRecorder()
		s.handleAPIBackupStop(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		if !trigger.stopCalled {
			t.Error("expected StopBackup to be called")
		}
		if !strings.Contains(w.Body.String(), "Stop signal sent to backup") {
			t.Errorf("unexpected body: %s", w.Body.String())
		}
	})

	t.Run("stop failure", func(t *testing.T) {
		trigger := &mockBackupTrigger{stopError: fmt.Errorf("some stop error")}
		s := &Server{
			backupTrigger: trigger,
		}
		req := httptest.NewRequest(http.MethodPost, "/api/backups/stop", nil)
		w := httptest.NewRecorder()
		s.handleAPIBackupStop(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		if !trigger.stopCalled {
			t.Error("expected StopBackup to be called")
		}
		if !strings.Contains(w.Body.String(), "Failed to stop backup: some stop error") {
			t.Errorf("unexpected body: %s", w.Body.String())
		}
	})
}
