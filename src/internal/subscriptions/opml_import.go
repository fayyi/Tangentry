package subscriptions

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"strings"
)

func (s *Service) ImportOPML(ctx context.Context, raw []byte) (OPMLImportResult, error) {
	if len(raw) == 0 || len(raw) > maxOPMLBytes {
		return OPMLImportResult{}, ErrInvalid
	}
	if err := validateOPML(ctx, raw); err != nil {
		return OPMLImportResult{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var document opmlDocument
	if err := decoder.Decode(&document); err != nil {
		return OPMLImportResult{}, ErrInvalid
	}
	outlines := flattenOPMLOutlines(document.Body.Outlines)
	if len(outlines) == 0 {
		return OPMLImportResult{}, ErrInvalid
	}
	result := OPMLImportResult{Items: make([]OPMLImportItem, 0, len(outlines))}
	for _, entry := range outlines {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		outline := entry.outline
		name := strings.TrimSpace(outline.Text)
		if name == "" {
			name = strings.TrimSpace(outline.Title)
		}
		address, inputType := opmlInput(outline)
		item := OPMLImportItem{Name: name, Address: address}
		if address == "" {
			item.Status, item.Reason = "skipped", "没有可导入的来源地址"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		source, created, err := s.Create(ctx, Input{Type: inputType, Query: address, Name: name})
		if err != nil {
			item.Status = "failed"
			item.Reason = opmlReason(err)
			result.Failed++
		} else if created {
			item.Status = "created"
			item.Name = source.Name
			result.Created++
			if entry.folder != "" && !s.restoreOPMLGroup(ctx, source.ID, entry.folder) {
				result.Ungrouped++
				item.Reason = "来源已导入，分组未恢复"
			}
		} else {
			item.Status = "existing"
			item.Name = source.Name
			result.Existing++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

type opmlEntry struct {
	outline opmlOutline
	folder  string
}

func flattenOPMLOutlines(values []opmlOutline) []opmlEntry {
	outlines := make([]opmlEntry, 0)
	var flatten func([]opmlOutline, string)
	flatten = func(values []opmlOutline, parent string) {
		for _, value := range values {
			if strings.TrimSpace(value.XMLURL) != "" || strings.TrimSpace(value.HTMLURL) != "" {
				outlines = append(outlines, opmlEntry{value, parent})
				flatten(value.Children, parent)
				continue
			}
			folder := strings.TrimSpace(value.Text)
			if folder == "" {
				folder = strings.TrimSpace(value.Title)
			}
			if parent != "" && folder != "" {
				folder = parent + " / " + folder
			}
			if folder == "" {
				folder = parent
			}
			flatten(value.Children, folder)
		}
	}
	flatten(values, "")
	return outlines
}

func (s *Service) restoreOPMLGroup(ctx context.Context, sourceID, folder string) bool {
	repository, supported := s.repository.(GroupRepository)
	if !supported {
		return false
	}
	groups := NewGroupService(repository)
	group, _, err := groups.Create(ctx, folder)
	if err != nil {
		return false
	}
	_, err = groups.Assign(ctx, sourceID, group.ID, "")
	return err == nil
}

func opmlInput(outline opmlOutline) (string, string) {
	if strings.EqualFold(strings.TrimSpace(outline.TangentryType), "bilibili") || strings.EqualFold(strings.TrimSpace(outline.Type), "bilibili") {
		return strings.TrimSpace(outline.HTMLURL), "bilibili"
	}
	return strings.TrimSpace(outline.XMLURL), "rss"
}

func opmlReason(err error) string {
	switch {
	case errors.Is(err, ErrInvalid):
		return "地址或来源类型不受支持"
	case errors.Is(err, ErrChannelInput):
		return "YouTube 频道地址无效"
	default:
		return "来源没有导入成功"
	}
}
