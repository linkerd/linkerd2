package prommatch

import (
	"fmt"
)

type matcherAndMessage struct {
	matcher *Matcher
	message string
	present bool
}

// Suite is a list of matchers and messages to check against a string of metrics.
type Suite []matcherAndMessage

// MustContain a series which will match the provided matcher.
func (ms Suite) MustContain(message string, m *Matcher) Suite {
	return append(ms, matcherAndMessage{
		matcher: m,
		message: message,
		present: true,
	})
}

// MustNotContain a series which will match the provided matcher.
func (ms Suite) MustNotContain(message string, m *Matcher) Suite {
	return append(ms, matcherAndMessage{
		matcher: m,
		message: message,
		present: false,
	})
}

// CheckString will run each assertion in the suite against the provided metrics.
func (ms Suite) CheckString(metrics string) error {
	for _, m := range ms {
		ok, err := m.matcher.HasMatchInString(metrics)
		if err != nil {
			return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
		}
		if m.present && !ok {
			return fmt.Errorf("expected to find %s\n%s", m.message, metrics)
		}
		if !m.present && ok {
			return fmt.Errorf("expected not to find %s\n%s", m.message, metrics)
		}
	}
	return nil
}
