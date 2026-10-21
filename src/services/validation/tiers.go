package validation

import "regexp"

var TierNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{2,31}$`)
