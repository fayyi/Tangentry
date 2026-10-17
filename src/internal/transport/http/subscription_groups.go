package httptransport

import (
	"net/http"
	"tangentry/internal/subscriptions"
)

func (h *feedsHandler) subscriptionGroupsReady(w http.ResponseWriter) bool {
	if h.feeds.SubscriptionGroups != nil {
		return true
	}
	write(w, 503, apiError{"subscription_groups_unavailable", "来源分组暂时不可用，仍可阅读已有来源。"})
	return false
}
func (h *feedsHandler) subscriptionGroupList(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionGroupsReady(w) {
		return
	}
	groups, err := h.feeds.SubscriptionGroups.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"groups": groups})
}
func (h *feedsHandler) subscriptionGroupCreate(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionGroupsReady(w) {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeSubscription(w, r, &input) {
		return
	}
	group, created, err := h.feeds.SubscriptionGroups.Create(r.Context(), input.Name)
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	write(w, status, map[string]any{"group": group, "created": created})
}
func (h *feedsHandler) subscriptionGroupRename(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionGroupsReady(w) {
		return
	}
	var input subscriptions.GroupEdit
	if !decodeSubscription(w, r, &input) {
		return
	}
	group, err := h.feeds.SubscriptionGroups.Rename(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, group)
}
func (h *feedsHandler) subscriptionGroupDelete(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionGroupsReady(w) {
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decodeSubscription(w, r, &input) {
		return
	}
	if err := h.feeds.SubscriptionGroups.Delete(r.Context(), r.PathValue("id"), input.Revision); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *feedsHandler) subscriptionGroupAssign(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionGroupsReady(w) {
		return
	}
	var input struct {
		GroupID         *string `json:"groupId"`
		ExpectedGroupID *string `json:"expectedGroupId"`
	}
	if !decodeSubscription(w, r, &input) {
		return
	}
	if input.GroupID == nil || input.ExpectedGroupID == nil {
		fail(w, subscriptions.ErrGroupInvalid)
		return
	}
	membership, err := h.feeds.SubscriptionGroups.Assign(r.Context(), r.PathValue("id"), *input.GroupID, *input.ExpectedGroupID)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, membership)
}
