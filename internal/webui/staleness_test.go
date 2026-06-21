package webui

import "testing"

func TestPollingStalenessState_InitialState(t *testing.T) {
	s := PollingStalenessState{}
	if s.IsStale() {
		t.Error("new state should not be stale")
	}
	if s.Failures() != 0 {
		t.Errorf("new state should have 0 failures, got %d", s.Failures())
	}
}

func TestPollingStalenessState_BecomeStaleAt3Failures(t *testing.T) {
	s := PollingStalenessState{}
	s.RecordFailure()
	if s.IsStale() {
		t.Error("should not be stale after 1 failure")
	}
	s.RecordFailure()
	if s.IsStale() {
		t.Error("should not be stale after 2 failures")
	}
	s.RecordFailure()
	if !s.IsStale() {
		t.Error("should be stale after 3 failures")
	}
	if s.Failures() != 3 {
		t.Errorf("expected 3 failures, got %d", s.Failures())
	}
}

func TestPollingStalenessState_SuccessResets(t *testing.T) {
	s := PollingStalenessState{}
	s.RecordFailure()
	s.RecordFailure()
	s.RecordFailure()
	if !s.IsStale() {
		t.Error("should be stale after 3 failures")
	}
	s.RecordSuccess()
	if s.IsStale() {
		t.Error("should not be stale after success")
	}
	if s.Failures() != 0 {
		t.Errorf("failures should be 0 after success, got %d", s.Failures())
	}
}

func TestPollingStalenessState_SuccessResetsBeforeStale(t *testing.T) {
	s := PollingStalenessState{}
	s.RecordFailure()
	s.RecordFailure()
	s.RecordSuccess()
	if s.IsStale() {
		t.Error("should not be stale after success before reaching 3")
	}
	if s.Failures() != 0 {
		t.Errorf("failures should be 0 after success, got %d", s.Failures())
	}
}

func TestPollingStalenessState_StaleRemainsAfterMoreFailures(t *testing.T) {
	s := PollingStalenessState{}
	for i := 0; i < 5; i++ {
		s.RecordFailure()
	}
	if !s.IsStale() {
		t.Error("should remain stale after more than 3 failures")
	}
	if s.Failures() != 5 {
		t.Errorf("expected 5 failures, got %d", s.Failures())
	}
}
