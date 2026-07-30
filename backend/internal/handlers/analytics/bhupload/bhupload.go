// Package bhupload streams a multipart BloodHound-collection upload and ingests it into an
// in-memory bloodhound.Collector WITHOUT writing anything to disk. It knows only about HTTP
// multipart parsing and the bloodhound parser — not about authentication, teams, or the database —
// so it can be shared by both the JWT (app) and API-key (user) analytics handlers.
package bhupload

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/bloodhound"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/mazrean/formstream"
)

// Fields holds the parsed report-creation metadata from the multipart form.
type Fields struct {
	ClientID       string
	HashlistIDs    []int64
	StartDate      time.Time
	EndDate        time.Time
	HasStartDate   bool
	HasEndDate     bool
	CustomPatterns []string
}

// Result is the outcome of parsing an upload: the metadata fields and a Collector holding the
// ingested AD graph. The caller seeds in-scope accounts (Collector.SetInScope) and calls Resolve().
type Result struct {
	Fields    Fields
	Collector *bloodhound.Collector
	FileCount int
}

// Parse streams the request body (which the caller MUST have already wrapped with
// http.MaxBytesReader) through a multipart parser. File parts ("file" or "files") are handed to the
// bloodhound parser; metadata parts are read with a 1 MiB cap. Nothing touches disk.
func Parse(r *http.Request, lim bloodhound.Limits) (*Result, error) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("invalid Content-Type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, fmt.Errorf("missing multipart boundary")
	}

	collector := bloodhound.NewCollector(nil, lim)
	res := &Result{Collector: collector}
	parser := formstream.NewParser(boundary)

	strField := func(dst *string) func(io.Reader, formstream.Header) error {
		return func(rd io.Reader, _ formstream.Header) error {
			v, err := httputil.ReadFormField(rd)
			if err != nil {
				return err
			}
			*dst = strings.TrimSpace(v)
			return nil
		}
	}

	var clientID, hashlistIDsRaw, startRaw, endRaw, patternsRaw string
	parser.Register("client_id", strField(&clientID))
	parser.Register("hashlist_ids", strField(&hashlistIDsRaw))
	parser.Register("start_date", strField(&startRaw))
	parser.Register("end_date", strField(&endRaw))
	parser.Register("custom_patterns", strField(&patternsRaw))

	fileCb := func(rd io.Reader, header formstream.Header) error {
		res.FileCount++
		return bloodhound.ParseInput(rd, header.FileName(), collector)
	}
	parser.Register("file", fileCb)
	parser.Register("files", fileCb)

	if err := parser.Parse(r.Body); err != nil {
		return nil, err
	}

	res.Fields.ClientID = clientID
	ids, err := parseInt64List(hashlistIDsRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid hashlist_ids: %w", err)
	}
	res.Fields.HashlistIDs = ids
	if startRaw != "" {
		t, err := time.Parse(time.RFC3339, startRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid start_date (want RFC3339): %w", err)
		}
		res.Fields.StartDate = t
		res.Fields.HasStartDate = true
	}
	if endRaw != "" {
		t, err := time.Parse(time.RFC3339, endRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid end_date (want RFC3339): %w", err)
		}
		res.Fields.EndDate = t
		res.Fields.HasEndDate = true
	}
	res.Fields.CustomPatterns = parseStringList(patternsRaw)
	return res, nil
}

// ScopeFromRefs builds the in-scope key set for a Collector from hashlist account identities.
func ScopeFromRefs(refs []models.AccountRef) map[string]struct{} {
	scope := make(map[string]struct{})
	for _, ref := range refs {
		dom := ""
		if ref.Domain != nil {
			dom = *ref.Domain
		}
		for _, k := range bloodhound.ScopeKeys(dom, ref.Username) {
			scope[k] = struct{}{}
		}
	}
	return scope
}

func parseInt64List(s string) ([]int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "[") {
		var ids []int64
		if err := json.Unmarshal([]byte(s), &ids); err != nil {
			return nil, err
		}
		return ids, nil
	}
	var ids []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, err
		}
		ids = append(ids, n)
	}
	return ids, nil
}

func parseStringList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "[") {
		var out []string
		if err := json.Unmarshal([]byte(s), &out); err == nil {
			return out
		}
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
