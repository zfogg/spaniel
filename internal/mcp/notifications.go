package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/model"
)

func (h *handler) registerNotificationTools(s *mcpsdk.Server) {
	addTOONTool(s, &mcpsdk.Tool{Name: "list_notifications", Description: "List the durable operator inbox across alerts and other Spaniel findings.", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}, h.listNotifications)
	if h.opts.AllowWrites {
		annotations := &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}
		addTOONTool(s, &mcpsdk.Tool{Name: "mark_notification_read", Description: "Mark one inbox notification as read.", Annotations: annotations}, h.markNotificationRead)
		addTOONTool(s, &mcpsdk.Tool{Name: "acknowledge_notification", Description: "Mark one inbox notification as read and acknowledged.", Annotations: annotations}, h.acknowledgeNotification)
	}
}

type ListNotificationsInput struct {
	Source string `json:"source,omitempty"`
	Page   int    `json:"page,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ListNotificationsOutput struct {
	Items []*model.NotificationRecord `json:"items"`
	Total int64                       `json:"total"`
	Page  int                         `json:"page"`
}

func (h *handler) listNotifications(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListNotificationsInput) (*mcpsdk.CallToolResult, ListNotificationsOutput, error) {
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	items, total, err := h.store.WithContext(ctx).ListNotifications(in.Source, page, limit)
	if err != nil {
		return nil, ListNotificationsOutput{}, err
	}
	return nil, ListNotificationsOutput{Items: items, Total: total, Page: page}, nil
}

type NotificationActionInput struct {
	NotificationID string `json:"notification_id"`
}

func (h *handler) markNotificationRead(ctx context.Context, _ *mcpsdk.CallToolRequest, in NotificationActionInput) (*mcpsdk.CallToolResult, MutationOutput, error) {
	if in.NotificationID == "" {
		return nil, MutationOutput{}, fmt.Errorf("notification_id is required")
	}
	if err := h.store.WithContext(ctx).MarkNotificationRead(in.NotificationID, false); err != nil {
		return nil, MutationOutput{}, err
	}
	return nil, MutationOutput{OK: true}, nil
}

func (h *handler) acknowledgeNotification(ctx context.Context, _ *mcpsdk.CallToolRequest, in NotificationActionInput) (*mcpsdk.CallToolResult, MutationOutput, error) {
	if in.NotificationID == "" {
		return nil, MutationOutput{}, fmt.Errorf("notification_id is required")
	}
	if err := h.store.WithContext(ctx).MarkNotificationRead(in.NotificationID, true); err != nil {
		return nil, MutationOutput{}, err
	}
	return nil, MutationOutput{OK: true}, nil
}
