//go:build linux

package pam

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// SessionCookieCtxKey is the context key for storing session cookies
const SessionCookieCtxKey contextKey = "session_cookie"
