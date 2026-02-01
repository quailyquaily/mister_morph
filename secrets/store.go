package secrets

import "strings"

type ProfileStore struct {
	byID map[string]AuthProfile
}

func NewProfileStore(profiles map[string]AuthProfile) *ProfileStore {
	cp := make(map[string]AuthProfile, len(profiles))
	for k, v := range profiles {
		cp[k] = v
	}
	return &ProfileStore{byID: cp}
}

func (s *ProfileStore) Get(id string) (AuthProfile, bool) {
	if s == nil || s.byID == nil {
		return AuthProfile{}, false
	}
	key := strings.TrimSpace(id)
	if key == "" {
		return AuthProfile{}, false
	}
	p, ok := s.byID[key]
	return p, ok
}
