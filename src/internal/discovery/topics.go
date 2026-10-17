package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrTopicInvalid  = errors.New("invalid discovery topic")
	ErrTopicNotFound = errors.New("discovery topic not found")
	ErrTopicConflict = errors.New("discovery topic changed")
)

var topicIDPattern = regexp.MustCompile(`^topic:[a-f0-9]{32}$`)

type TopicRule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Keywords  []string `json:"keywords"`
	GroupID   string   `json:"groupId,omitempty"`
	Revision  int64    `json:"revision"`
	CreatedAt int64    `json:"createdAt"`
}

type TopicRuleEdit struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
	GroupID  string   `json:"groupId,omitempty"`
	Revision int64    `json:"revision"`
}

type TopicRuleRepository interface {
	ListDiscoveryTopics(context.Context) ([]TopicRule, error)
	CreateDiscoveryTopic(context.Context, TopicRule) (TopicRule, error)
	UpdateDiscoveryTopic(context.Context, string, TopicRuleEdit) (TopicRule, error)
	DeleteDiscoveryTopic(context.Context, string, int64) error
}

type TopicRules struct{ repository TopicRuleRepository }

func NewTopicRules(repository TopicRuleRepository) *TopicRules {
	return &TopicRules{repository: repository}
}

func (s *TopicRules) List(ctx context.Context) ([]TopicRule, error) {
	if s == nil || s.repository == nil {
		return nil, ErrUnavailable
	}
	return s.repository.ListDiscoveryTopics(ctx)
}

func (s *TopicRules) Create(ctx context.Context, edit TopicRuleEdit) (TopicRule, error) {
	name, keywords, groupID, err := validateTopic(edit.Name, edit.Keywords, edit.GroupID)
	if err != nil {
		return TopicRule{}, err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return TopicRule{}, err
	}
	return s.repository.CreateDiscoveryTopic(ctx, TopicRule{ID: "topic:" + hex.EncodeToString(raw[:]), Name: name, Keywords: keywords, GroupID: groupID, Revision: 1})
}

func (s *TopicRules) Update(ctx context.Context, id string, edit TopicRuleEdit) (TopicRule, error) {
	if !validTopicID(id) || edit.Revision < 1 {
		return TopicRule{}, ErrTopicInvalid
	}
	name, keywords, groupID, err := validateTopic(edit.Name, edit.Keywords, edit.GroupID)
	if err != nil {
		return TopicRule{}, err
	}
	edit.Name, edit.Keywords, edit.GroupID = name, keywords, groupID
	return s.repository.UpdateDiscoveryTopic(ctx, id, edit)
}

func (s *TopicRules) Delete(ctx context.Context, id string, revision int64) error {
	if !validTopicID(id) || revision < 1 {
		return ErrTopicInvalid
	}
	return s.repository.DeleteDiscoveryTopic(ctx, id, revision)
}

func validateTopic(name string, keywords []string, groupID string) (string, []string, string, error) {
	if !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", nil, "", ErrTopicInvalid
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 60 || len(keywords) == 0 || len(keywords) > 20 {
		return "", nil, "", ErrTopicInvalid
	}
	unique := make(map[string]struct{}, len(keywords))
	clean := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" || utf8.RuneCountInString(keyword) > 80 || strings.IndexFunc(keyword, unicode.IsControl) >= 0 {
			return "", nil, "", ErrTopicInvalid
		}
		key := strings.ToLower(keyword)
		if _, ok := unique[key]; ok {
			continue
		}
		unique[key] = struct{}{}
		clean = append(clean, keyword)
	}
	if len(clean) == 0 || len(groupID) > 128 {
		return "", nil, "", ErrTopicInvalid
	}
	sort.Strings(clean)
	return name, clean, strings.TrimSpace(groupID), nil
}

func validTopicID(id string) bool {
	return topicIDPattern.MatchString(id)
}
