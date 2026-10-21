package api

// IamMutation describes registered operations only; failed/unsupported requests
// never invalidate access. Runtime dispatch reads the same operation metadata.
func IamMutation(method, path string) bool {
	match := registry.Match(method, "/api/sessions/lookup/"+path)
	return match.MethodAllowed && match.Operation.InvalidatesAccess
}
