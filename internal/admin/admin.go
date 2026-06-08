package admin

import "slices"

type Access struct {
	platformIDs []string
}

func NewAccess(platformIDs []string) Access {
	return Access{platformIDs: platformIDs}
}

func (a Access) IsAdmin(platformID string) bool {
	return slices.Contains(a.platformIDs, platformID)
}

