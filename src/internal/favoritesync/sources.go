package favoritesync

import (
	"context"
	"errors"
	"time"

	"tangentry/internal/content"
)

func (s *Service) Create(ctx context.Context, input Input) (Source, error) {
	if emptyPolicy(input.Policy) {
		input.Policy = DefaultPolicy()
	}
	input.Policy = normalizedPolicy(input.Policy)
	id, err := ParseFolder(input.Query)
	if err != nil || !validPolicy(input.Policy) {
		return Source{}, ErrInvalid
	}
	release, err := s.begin(id)
	if err != nil {
		return Source{}, err
	}
	defer release()
	existing, err := s.repo.GetFavoriteSource(ctx, id)
	if err == nil {
		return NormalizeSource(existing), nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Source{}, err
	}
	source := Source{
		FolderID:  id,
		Title:     "收藏夹 " + id,
		Policy:    input.Policy,
		Items:     []content.Item{},
		Pending:   []string{},
		Transfers: map[string]Transfer{},
		CreatedAt: time.Now().Unix(),
	}
	if err := s.repo.PutFavoriteSource(ctx, source); err != nil {
		return Source{}, err
	}
	return source, nil
}

func emptyPolicy(p Policy) bool {
	return !p.Enabled && !p.AutoDownload && p.IntervalMinutes == 0 && p.MaxPerRun == 0 && p.MaxHeight == 0 &&
		emptyDownloadOptions(p.Download) && !p.DownloadSubtitles && p.DirectoryName == "" &&
		p.TitleInclude == "" && p.TitleExclude == "" && p.AuthorInclude == ""
}

func (s *Service) Get(ctx context.Context, id string) (Source, error) {
	if !folderID.MatchString(id) {
		return Source{}, ErrInvalid
	}
	source, err := s.repo.GetFavoriteSource(ctx, id)
	source = NormalizeSource(source)
	s.mu.Lock()
	source.Active = s.activeID == id
	s.mu.Unlock()
	return source, err
}

func (s *Service) Configure(ctx context.Context, id string, p Policy) (Source, error) {
	p = normalizedPolicy(p)
	if !folderID.MatchString(id) || !validPolicy(p) {
		return Source{}, ErrInvalid
	}
	release, err := s.begin(id)
	if err != nil {
		return Source{}, err
	}
	defer release()
	source, err := s.repo.GetFavoriteSource(ctx, id)
	if err != nil {
		return Source{}, err
	}
	source = NormalizeSource(source)
	if p.DirectoryName == "" && source.Title != "收藏夹 "+source.FolderID {
		p.DirectoryName = folderDirectoryName(source.Title, source.FolderID)
	}
	source.Policy = p
	source.Pending = matchingPending(p, source.Items, source.Pending)
	if err := s.repo.PutFavoriteSource(ctx, source); err != nil {
		return Source{}, err
	}
	return source, nil
}

func (s *Service) Remove(ctx context.Context, id string) error {
	if !folderID.MatchString(id) {
		return ErrInvalid
	}
	release, err := s.begin(id)
	if err != nil {
		return err
	}
	defer release()
	return s.repo.DeleteFavoriteSource(ctx, id)
}

func (s *Service) Start(ctx context.Context, id string) (Source, error) {
	if !folderID.MatchString(id) {
		return Source{}, ErrInvalid
	}
	release, err := s.begin(id)
	if err != nil {
		return Source{}, err
	}
	if err := s.checkRestriction(ctx, time.Now()); err != nil {
		release()
		return Source{}, err
	}
	source, err := s.repo.GetFavoriteSource(ctx, id)
	if err != nil {
		release()
		return Source{}, err
	}
	source = NormalizeSource(source)
	source.LastAttemptAt = time.Now().Unix()
	source.Message = "正在读取收藏夹…"
	source.Active = false
	if err := s.repo.PutFavoriteSource(ctx, source); err != nil {
		release()
		return Source{}, err
	}
	run, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	source.Active = true
	go func() { defer release(); defer cancel(); s.run(run, source) }()
	return source, nil
}
