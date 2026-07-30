package bloodhound

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// FileMeta is the "meta" object present in every BloodHound JSON file.
type FileMeta struct {
	Type    string `json:"type"`
	Count   int    `json:"count"`
	Version int    `json:"version"`
	Methods int64  `json:"methods"`
}

// props is a BloodHound "Properties" object with nil-safe, type-tolerant accessors. Decoding into
// a generic map absorbs property-name and value-type drift across SharpHound v3/v4/v5/v6 and BHCE.
type props map[string]any

func (p props) str(key string) string {
	if p == nil {
		return ""
	}
	switch v := p[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func (p props) boolean(key string) bool {
	if p == nil {
		return false
	}
	switch v := p[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	case float64:
		return v != 0
	case json.Number:
		f, _ := v.Float64()
		return f != 0
	default:
		return false
	}
}

// isTierZero folds the several signals BloodHound versions use to mark high-value / tier-0 objects:
// legacy "highvalue", BHCE "isTierZero", and a "system_tags" string containing "admin_tier_0".
func (p props) isTierZero() bool {
	if p.boolean("highvalue") || p.boolean("isTierZero") {
		return true
	}
	if tags := p.str("system_tags"); tags != "" && strings.Contains(tags, "admin_tier_0") {
		return true
	}
	return false
}

// RawUser is a "users" element.
type RawUser struct {
	ObjectIdentifier  string      `json:"ObjectIdentifier"` // SID
	Properties        props       `json:"Properties"`
	PrimaryGroupSID   string      `json:"PrimaryGroupSID"`
	Aces              []RawAce    `json:"Aces"`
	AllowedToDelegate []RawMember `json:"AllowedToDelegate"`
}

// RawGroup is a "groups" element.
type RawGroup struct {
	ObjectIdentifier string      `json:"ObjectIdentifier"`
	Properties       props       `json:"Properties"`
	Members          []RawMember `json:"Members"`
	Aces             []RawAce    `json:"Aces"`
}

// RawComputer is a "computers" element.
type RawComputer struct {
	ObjectIdentifier   string          `json:"ObjectIdentifier"`
	Properties         props           `json:"Properties"`
	PrimaryGroupSID    string          `json:"PrimaryGroupSID"`
	Aces               []RawAce        `json:"Aces"`
	LocalAdmins        RawPrincipalSet `json:"LocalAdmins"`
	AdminRights        RawPrincipalSet `json:"AdminRights"` // legacy alternate for LocalAdmins
	RemoteDesktopUsers RawPrincipalSet `json:"RemoteDesktopUsers"`
	PSRemoteUsers      RawPrincipalSet `json:"PSRemoteUsers"`
	DcomUsers          RawPrincipalSet `json:"DcomUsers"`
	Sessions           RawPrincipalSet `json:"Sessions"`
	AllowedToAct       RawPrincipalSet `json:"AllowedToAct"`
	AllowedToDelegate  []RawMember     `json:"AllowedToDelegate"`
}

// RawDomain is a "domains" element (used for DCSync ACE detection).
type RawDomain struct {
	ObjectIdentifier string   `json:"ObjectIdentifier"`
	Properties       props    `json:"Properties"`
	Aces             []RawAce `json:"Aces"`
}

// RawMember tolerates modern ObjectIdentifier/ObjectType and legacy MemberId/MemberType.
type RawMember struct {
	ObjectIdentifier string
	ObjectType       string
}

func (m *RawMember) UnmarshalJSON(b []byte) error {
	// Older collectors emit a bare SID string instead of a typed object.
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '"' {
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return err
		}
		m.ObjectIdentifier = s
		return nil
	}
	var raw struct {
		ObjectIdentifier string `json:"ObjectIdentifier"`
		ObjectType       string `json:"ObjectType"`
		MemberID         string `json:"MemberId"`
		MemberType       string `json:"MemberType"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.ObjectIdentifier = raw.ObjectIdentifier
	if m.ObjectIdentifier == "" {
		m.ObjectIdentifier = raw.MemberID
	}
	m.ObjectType = raw.ObjectType
	if m.ObjectType == "" {
		m.ObjectType = raw.MemberType
	}
	return nil
}

// RawAce tolerates PrincipalSID/PrincipalID and RightName/RightsName.
type RawAce struct {
	PrincipalSID  string
	PrincipalType string
	RightName     string
	IsInherited   bool
}

func (a *RawAce) UnmarshalJSON(b []byte) error {
	var raw struct {
		PrincipalSID  string `json:"PrincipalSID"`
		PrincipalID   string `json:"PrincipalID"`
		PrincipalType string `json:"PrincipalType"`
		RightName     string `json:"RightName"`
		RightsName    string `json:"RightsName"`
		IsInherited   bool   `json:"IsInherited"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.PrincipalSID = raw.PrincipalSID
	if a.PrincipalSID == "" {
		a.PrincipalSID = raw.PrincipalID
	}
	a.PrincipalType = raw.PrincipalType
	a.RightName = raw.RightName
	if a.RightName == "" {
		a.RightName = raw.RightsName
	}
	a.IsInherited = raw.IsInherited
	return nil
}

// RawPrincipalSet is SharpHound's {Collected, FailureReason, Results:[...]} wrapper. It also
// tolerates a bare array (older collectors) via custom unmarshal.
type RawPrincipalSet struct {
	Results []RawMember `json:"Results"`
}

func (s *RawPrincipalSet) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '[' {
		return json.Unmarshal(b, &s.Results)
	}
	var obj struct {
		Results []RawMember `json:"Results"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	s.Results = obj.Results
	return nil
}
