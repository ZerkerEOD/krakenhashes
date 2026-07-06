package models

import "errors"

var (
	ErrTeamNotFound         = errors.New("team not found")
	ErrNotTeamMember        = errors.New("user is not a member of this team")
	ErrLastTeamAdmin        = errors.New("cannot remove or demote the last admin of a team")
	ErrDefaultTeamProtected = errors.New("the Default Team cannot be deleted or modified")
	ErrClientNotInTeam      = errors.New("client is not assigned to this team")
)
