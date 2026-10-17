package subscriptions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrGroupInvalid  = errors.New("invalid subscription group")
	ErrGroupNotFound = errors.New("subscription group not found")
	ErrGroupConflict = errors.New("subscription group changed")
	ErrGroupName     = errors.New("subscription group name already exists")
	ErrGroupLimit    = errors.New("subscription group limit reached")
)

type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	Revision  int64  `json:"revision"`
}
type GroupEdit struct {
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
}
type Membership struct {
	SourceID string `json:"sourceId"`
	GroupID  string `json:"groupId"`
}

// GroupRepository changes organization only. Source snapshots and refresh
// records remain owned by Repository; membership writes must be atomic.
type GroupRepository interface {
	ListSubscriptionGroups(context.Context) ([]Group, error)
	CreateSubscriptionGroup(context.Context, Group, string) (Group, bool, error)
	RenameSubscriptionGroup(context.Context, string, GroupEdit, string) (Group, error)
	DeleteSubscriptionGroup(context.Context, string, int64) error
	AssignSubscriptionGroup(context.Context, string, string, string) (Membership, error)
}
type GroupService struct{ repository GroupRepository }

func NewGroupService(repository GroupRepository) *GroupService {
	return &GroupService{repository: repository}
}

var groupID = regexp.MustCompile(`^group:[a-f0-9]{32}$`)

func groupName(raw string) (string, string, error) {
	if !utf8.ValidString(raw) || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", "", ErrGroupInvalid
	}
	label := strings.TrimSpace(raw)
	if label == "" || utf8.RuneCountInString(label) > 60 {
		return "", "", ErrGroupInvalid
	}
	return label, strings.ToLower(label), nil
}
func (s *GroupService) List(ctx context.Context) ([]Group, error) {
	return s.repository.ListSubscriptionGroups(ctx)
}
func (s *GroupService) Create(ctx context.Context, raw string) (Group, bool, error) {
	label, key, err := groupName(raw)
	if err != nil {
		return Group{}, false, err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return Group{}, false, err
	}
	return s.repository.CreateSubscriptionGroup(ctx, Group{ID: "group:" + hex.EncodeToString(random[:]), Name: label, CreatedAt: time.Now().Unix(), Revision: 1}, key)
}
func (s *GroupService) Rename(ctx context.Context, id string, edit GroupEdit) (Group, error) {
	label, key, err := groupName(edit.Name)
	if err != nil || !groupID.MatchString(id) || edit.Revision < 1 {
		return Group{}, ErrGroupInvalid
	}
	edit.Name = label
	return s.repository.RenameSubscriptionGroup(ctx, id, edit, key)
}
func (s *GroupService) Delete(ctx context.Context, id string, revision int64) error {
	if !groupID.MatchString(id) || revision < 1 {
		return ErrGroupInvalid
	}
	return s.repository.DeleteSubscriptionGroup(ctx, id, revision)
}
func (s *GroupService) Assign(ctx context.Context, source, group, expected string) (Membership, error) {
	if !validID(source) || (group != "" && !groupID.MatchString(group)) || (expected != "" && !groupID.MatchString(expected)) {
		return Membership{}, ErrGroupInvalid
	}
	return s.repository.AssignSubscriptionGroup(ctx, source, group, expected)
}
