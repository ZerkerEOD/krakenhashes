package auth

import "testing"

// TestRememberTokenMinutes verifies the single source of truth for a login/refresh
// lifetime: a normal session keeps the short JWT expiry; a "remember me" session
// uses the absolute timeout, or a 30-day default when none is configured.
func TestRememberTokenMinutes(t *testing.T) {
	const jwtExpiry = 15 // minutes

	tests := []struct {
		name            string
		absoluteTimeout int // hours
		remember        bool
		want            int // minutes
	}{
		{name: "normal session uses jwt expiry", absoluteTimeout: 0, remember: false, want: jwtExpiry},
		{name: "normal session ignores absolute timeout", absoluteTimeout: 48, remember: false, want: jwtExpiry},
		{name: "remember uses absolute timeout when set", absoluteTimeout: 48, remember: true, want: 48 * 60},
		{name: "remember falls back to 30d default", absoluteTimeout: 0, remember: true, want: defaultRememberMinutes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RememberTokenMinutes(jwtExpiry, tt.absoluteTimeout, tt.remember)
			if got != tt.want {
				t.Fatalf("RememberTokenMinutes(%d,%d,%v) = %d, want %d",
					jwtExpiry, tt.absoluteTimeout, tt.remember, got, tt.want)
			}
		})
	}
}

// TestAuthCookieMaxAge verifies a normal login is a browser-close session cookie
// (MaxAge 0) while a "remember me" login persists for the full token lifetime.
func TestAuthCookieMaxAge(t *testing.T) {
	if got := AuthCookieMaxAge(15, false); got != 0 {
		t.Fatalf("non-remember cookie MaxAge = %d, want 0 (session cookie)", got)
	}
	if got := AuthCookieMaxAge(60, true); got != 60*60 {
		t.Fatalf("remember cookie MaxAge = %d, want %d", got, 60*60)
	}
}
