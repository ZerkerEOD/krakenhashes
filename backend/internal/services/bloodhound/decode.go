package bloodhound

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseInput ingests one uploaded part into the collector. A ".zip" is buffered in memory (archive/zip
// needs random access) and every ".json" entry decoded; any other filename is treated as a single
// JSON document. Nothing is ever written to disk.
func ParseInput(r io.Reader, filename string, coll *Collector) error {
	if strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return parseZip(r, coll)
	}
	return decodeOneDocument(r, typeFromFilename(filename), coll)
}

func parseZip(r io.Reader, coll *Collector) error {
	buf, err := io.ReadAll(io.LimitReader(r, coll.lim.MaxZipBytes+1))
	if err != nil {
		return err
	}
	if int64(len(buf)) > coll.lim.MaxZipBytes {
		return ErrTooLarge
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return fmt.Errorf("bloodhound: invalid zip: %w", err)
	}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		cr := &countingReader{r: io.LimitReader(rc, coll.lim.MaxEntryBytes+1)}
		derr := decodeOneDocument(cr, typeFromFilename(name), coll)
		rc.Close()
		if derr != nil {
			return derr
		}
		if cr.n > coll.lim.MaxEntryBytes {
			return ErrTooLarge
		}
		total += cr.n
		if total > coll.lim.MaxTotalDecompressed {
			return ErrTooLarge
		}
	}
	return nil
}

// decodeOneDocument token-streams a single BloodHound JSON document ({data:[...], meta:{...}}).
// It handles meta appearing before OR after data: when the element type is not yet known (no
// filename hint and meta not seen), data elements are buffered as raw bytes and replayed once meta
// resolves the type.
func decodeOneDocument(r io.Reader, typeHint string, coll *Collector) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := expectDelim(dec, '{'); err != nil {
		if err == io.EOF {
			return nil // empty document
		}
		return err
	}
	docType := normalizeType(typeHint)
	var buffered []json.RawMessage
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		switch key {
		case "meta":
			var meta FileMeta
			if err := dec.Decode(&meta); err != nil {
				return err
			}
			if docType == "" {
				docType = normalizeType(meta.Type)
			}
		case "data":
			if err := expectDelim(dec, '['); err != nil {
				return err
			}
			for dec.More() {
				if docType != "" {
					if err := coll.decodeElement(docType, dec); err != nil {
						return err
					}
				} else {
					var raw json.RawMessage
					if err := dec.Decode(&raw); err != nil {
						return err
					}
					buffered = append(buffered, raw)
				}
			}
			if err := expectDelim(dec, ']'); err != nil {
				return err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil && err != io.EOF { // consume closing '}'
		return err
	}
	if len(buffered) > 0 && docType != "" {
		for _, raw := range buffered {
			if err := coll.decodeElementBytes(docType, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Collector) decodeElement(docType string, dec *json.Decoder) error {
	switch docType {
	case "users":
		var u RawUser
		if err := dec.Decode(&u); err != nil {
			return err
		}
		c.addUser(&u)
	case "groups":
		var g RawGroup
		if err := dec.Decode(&g); err != nil {
			return err
		}
		c.addGroup(&g)
	case "computers":
		var comp RawComputer
		if err := dec.Decode(&comp); err != nil {
			return err
		}
		c.addComputer(&comp)
	case "domains":
		var d RawDomain
		if err := dec.Decode(&d); err != nil {
			return err
		}
		c.addDomain(&d)
	default:
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) decodeElementBytes(docType string, raw []byte) error {
	switch docType {
	case "users":
		var u RawUser
		if err := json.Unmarshal(raw, &u); err != nil {
			return err
		}
		c.addUser(&u)
	case "groups":
		var g RawGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			return err
		}
		c.addGroup(&g)
	case "computers":
		var comp RawComputer
		if err := json.Unmarshal(raw, &comp); err != nil {
			return err
		}
		c.addComputer(&comp)
	case "domains":
		var d RawDomain
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		c.addDomain(&d)
	}
	return nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return fmt.Errorf("bloodhound: expected %q, got %v", want, tok)
	}
	return nil
}

func normalizeType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "users", "user":
		return "users"
	case "groups", "group":
		return "groups"
	case "computers", "computer":
		return "computers"
	case "domains", "domain":
		return "domains"
	default:
		return ""
	}
}

func typeFromFilename(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "users"):
		return "users"
	case strings.Contains(n, "groups"):
		return "groups"
	case strings.Contains(n, "computers"):
		return "computers"
	case strings.Contains(n, "domains"):
		return "domains"
	default:
		return ""
	}
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
