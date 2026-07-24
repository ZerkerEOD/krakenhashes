package scheduler

import (
	"testing"

	"github.com/google/uuid"
)

func TestClassifyClientWordlistPath(t *testing.T) {
	clientUUID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name         string
		path         string
		wantIsClient bool
		wantClientID string // expected UUID string when wantIsClient; ignored otherwise
		wantFilename string
	}{
		{
			name:         "client wordlist",
			path:         "wordlists/clients/" + clientUUID + "/rockyou.txt",
			wantIsClient: true,
			wantClientID: clientUUID,
			wantFilename: "rockyou.txt",
		},
		{
			name:         "client potfile",
			path:         "wordlists/clients/" + clientUUID + "/potfile.txt",
			wantIsClient: true,
			wantClientID: clientUUID,
			wantFilename: "potfile.txt",
		},
		{
			name:         "global wordlist",
			path:         "wordlists/general/rockyou.txt",
			wantIsClient: false,
		},
		{
			name:         "custom wordlist",
			path:         "wordlists/custom/x.txt",
			wantIsClient: false,
		},
		{
			name:         "association wordlist",
			path:         "wordlists/association/5_x.txt",
			wantIsClient: false,
		},
		{
			name:         "malformed uuid",
			path:         "wordlists/clients/not-a-uuid/x.txt",
			wantIsClient: false,
		},
		{
			name:         "wrong prefix",
			path:         "rules/clients/" + clientUUID + "/x.txt",
			wantIsClient: false,
		},
		{
			name:         "too few segments",
			path:         "wordlists/clients/" + clientUUID,
			wantIsClient: false,
		},
		{
			name:         "empty filename",
			path:         "wordlists/clients/" + clientUUID + "/",
			wantIsClient: false,
		},
		{
			name:         "empty path",
			path:         "",
			wantIsClient: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isClient, clientID, filename := classifyClientWordlistPath(tt.path)

			if isClient != tt.wantIsClient {
				t.Fatalf("isClient = %v, want %v", isClient, tt.wantIsClient)
			}

			if !tt.wantIsClient {
				// Non-client paths must return the zero values.
				if clientID != uuid.Nil {
					t.Errorf("clientID = %s, want uuid.Nil", clientID)
				}
				if filename != "" {
					t.Errorf("filename = %q, want empty", filename)
				}
				return
			}

			if clientID.String() != tt.wantClientID {
				t.Errorf("clientID = %s, want %s", clientID, tt.wantClientID)
			}
			if filename != tt.wantFilename {
				t.Errorf("filename = %q, want %q", filename, tt.wantFilename)
			}
		})
	}
}
