package httptransport

import (
	"context"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/favoritesync"
	"tangentry/internal/subscriptions"
)

type SubscriptionService interface {
	List(context.Context) ([]subscriptions.Source, error)
	Get(context.Context, string) (subscriptions.Detail, error)
	Create(context.Context, subscriptions.Input) (subscriptions.Source, bool, error)
	Rename(context.Context, string, subscriptions.Edit) (subscriptions.Source, error)
	EditAddress(context.Context, string, subscriptions.AddressEdit) (subscriptions.Source, error)
	Delete(context.Context, string) error
	Update(context.Context, string) (subscriptions.Detail, error)
	ConfigureRefresh(context.Context, string, subscriptions.RefreshPolicy) (subscriptions.Source, error)
	ResolveYouTube(context.Context, string) (discovery.YouTubeChannel, error)
	Resolve(context.Context, string) (subscriptions.ResolvedSource, error)
	ExportOPML(context.Context) ([]byte, error)
	ImportOPML(context.Context, []byte) (subscriptions.OPMLImportResult, error)
}

type GroupService interface {
	List(context.Context) ([]subscriptions.Group, error)
	Create(context.Context, string) (subscriptions.Group, bool, error)
	Rename(context.Context, string, subscriptions.GroupEdit) (subscriptions.Group, error)
	Delete(context.Context, string, int64) error
	Assign(context.Context, string, string, string) (subscriptions.Membership, error)
}

type InboxService interface {
	List(context.Context, subscriptions.InboxQuery) (subscriptions.InboxPage, error)
	Item(context.Context, string) (content.Item, error)
}

type FavoriteService interface {
	Folders(context.Context) ([]favoritesync.Folder, error)
	List(context.Context) ([]favoritesync.Source, error)
	Get(context.Context, string) (favoritesync.Source, error)
	Create(context.Context, favoritesync.Input) (favoritesync.Source, error)
	Configure(context.Context, string, favoritesync.Policy) (favoritesync.Source, error)
	Remove(context.Context, string) error
	Start(context.Context, string) (favoritesync.Source, error)
	Cancel(string) error
}
