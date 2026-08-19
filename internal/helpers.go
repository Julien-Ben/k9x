// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package internal

import (
	"regexp"
	"strings"

	"github.com/derailed/k9s/internal/view/cmd"
)

var (
	fuzzyRx = regexp.MustCompile(`\A-f\s?([\w-]+)\b`)
	labelRx = regexp.MustCompile(`\A\-l`)
	ctxRx   = regexp.MustCompile(`\A!?ctx=(\S+)\z`)
)

// Helpers...

// IsInverseSelector checks if inverse char has been provided.
func IsInverseSelector(s string) bool {
	if s == "" {
		return false
	}
	return s[0] == '!'
}

// IsLabelSelector checks if query is a label query.
func IsLabelSelector(s string) bool {
	if labelRx.MatchString(s) {
		return true
	}

	return !strings.Contains(s, " ") && cmd.ToLabels(s) != nil
}

// IsFuzzySelector checks if query is fuzzy.
func IsFuzzySelector(s string) (string, bool) {
	mm := fuzzyRx.FindStringSubmatch(s)
	if len(mm) != 2 {
		return "", false
	}

	return mm[1], true
}

// IsContextSelector matches a multi-context row filter of the form `ctx=NAME`
// (keep only rows from NAME) or `!ctx=NAME` (drop rows from NAME). Returns
// the target context name, whether the match is inverted, and ok=true when
// the input parsed as a context selector.
func IsContextSelector(s string) (string, bool, bool) {
	mm := ctxRx.FindStringSubmatch(s)
	if len(mm) != 2 {
		return "", false, false
	}
	return mm[1], strings.HasPrefix(s, "!"), true
}
