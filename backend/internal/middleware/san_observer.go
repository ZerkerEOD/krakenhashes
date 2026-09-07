package middleware

import (
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/sandiscovery"
	"github.com/gorilla/mux"
)

// SANObserver records the Host header of inbound requests as candidate
// certificate names.
//
// This is the passive half of discovery. It works because the Host header
// survives the network path that defeats interface enumeration:
//
//   - A browser reaching nginx on :443 has its address forwarded by
//     `proxy_set_header Host $host`, so the address the operator typed arrives
//     here intact.
//   - An agent connects directly to :31337, bypassing nginx entirely, so the
//     Host header is exactly what that agent dialled.
//   - An agent bootstrapping fetches /ca.crt over plain HTTP on :1337. For an
//     agent whose TLS is already broken that request is the ONLY passive
//     observation this server will ever get, which is why this middleware must
//     be registered on the HTTP router as well as the HTTPS one.
//
// The recorded value is attacker-influenceable -- anyone who can reach this
// server chooses what Host they send -- so it is never applied automatically and
// the UI labels its provenance.
func SANObserver(cache *sandiscovery.Cache) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cache != nil && r.Host != "" {
				cache.Observe(sandiscovery.Observation{
					Address:   r.Host,
					Source:    sandiscovery.SourceHostHeader,
					UserAgent: r.UserAgent(),
				})
			}
			next.ServeHTTP(w, r)
		})
	}
}
