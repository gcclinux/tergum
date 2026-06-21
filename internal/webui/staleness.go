package webui

// PollingStalenessState tracks consecutive polling failures for a section
// and determines whether the section should be marked as stale.
// Staleness triggers at 3 or more consecutive failures and clears on any success.
type PollingStalenessState struct {
	failures int
	stale    bool
}

// RecordFailure increments the consecutive failure counter.
// If the counter reaches 3, the section is marked stale.
func (s *PollingStalenessState) RecordFailure() {
	s.failures++
	if s.failures >= 3 {
		s.stale = true
	}
}

// RecordSuccess resets the consecutive failure counter to 0 and clears staleness.
func (s *PollingStalenessState) RecordSuccess() {
	s.failures = 0
	s.stale = false
}

// IsStale returns whether the section is currently marked as stale.
func (s *PollingStalenessState) IsStale() bool {
	return s.stale
}

// Failures returns the current consecutive failure count.
func (s *PollingStalenessState) Failures() int {
	return s.failures
}
