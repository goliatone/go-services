package jira

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/goliatone/go-services/core"
)

type adfNode struct {
	Type    string         `json:"type"`
	Version int            `json:"version,omitempty"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []adfNode      `json:"content,omitempty"`
	Marks   []struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs,omitempty"`
	} `json:"marks,omitempty"`
}

func discussionBody(raw json.RawMessage) (string, error) {
	var document adfNode
	if json.Unmarshal(raw, &document) != nil || document.Type != "doc" || document.Version != 1 || len(document.Content) == 0 {
		return "", discussionError(core.TrackerErrorExternal, "Jira comment requires an ADF document")
	}
	body, err := adfText(document, 0)
	if err != nil {
		return "", err
	}
	if body == "" || !utf8.ValidString(body) || len(body) > core.TrackerDiscussionMaxBodyBytes {
		return "", discussionError(core.TrackerErrorExternal, "Jira comment body is empty or exceeds the normalized bound")
	}
	return body, nil
}

func adfText(node adfNode, depth int) (string, error) {
	if depth > 32 {
		return "", discussionError(core.TrackerErrorExternal, "Jira comment document is too deeply nested")
	}
	switch node.Type {
	case "text":
		var text strings.Builder
		text.WriteString(node.Text)
		for _, mark := range node.Marks {
			switch mark.Type {
			case "link":
				href, _ := mark.Attrs["href"].(string)
				if href == "" {
					return "", discussionError(core.TrackerErrorExternal, "Jira comment link omits its URL")
				}
				if href != node.Text {
					text.WriteString(" (" + href + ")")
				}
			case "strong", "em", "strike", "underline", "code", "subsup", "textColor", "backgroundColor":
			default:
				return "", discussionError(core.TrackerErrorUnavailable, "Jira comment contains an unsupported text mark")
			}
		}
		return text.String(), nil
	case "hardBreak":
		return "\n", nil
	case "mention":
		id, _ := node.Attrs["id"].(string)
		text, _ := node.Attrs["text"].(string)
		if id == "" {
			return "", discussionError(core.TrackerErrorExternal, "Jira mention omits its identity")
		}
		if text == "" {
			text = "@" + id
		}
		return text, nil
	case "emoji":
		text, _ := node.Attrs["text"].(string)
		if text == "" {
			text, _ = node.Attrs["shortName"].(string)
		}
		if text == "" {
			return "", discussionError(core.TrackerErrorExternal, "Jira emoji omits text")
		}
		return text, nil
	case "inlineCard", "blockCard":
		value, _ := node.Attrs["url"].(string)
		if value == "" {
			return "", discussionError(core.TrackerErrorUnavailable, "Jira card has no text URL")
		}
		return value, nil
	case "rule":
		return "---", nil
	case "doc", "paragraph", "heading", "blockquote", "codeBlock", "bulletList", "orderedList", "listItem", "panel", "table", "tableRow", "tableCell", "tableHeader":
		separator := "\n"
		if node.Type == "paragraph" || node.Type == "heading" || node.Type == "codeBlock" {
			separator = ""
		}
		if node.Type == "tableRow" {
			separator = "\t"
		}
		parts := make([]string, 0, len(node.Content))
		size := 0
		for _, child := range node.Content {
			text, err := adfText(child, depth+1)
			if err != nil {
				return "", err
			}
			size += len(text)
			if len(parts) > 0 {
				size += len(separator)
			}
			if size > core.TrackerDiscussionMaxBodyBytes {
				return "", discussionError(core.TrackerErrorExternal, "Jira normalized document exceeds its bound")
			}
			parts = append(parts, text)
		}
		return strings.Join(parts, separator), nil
	default:
		return "", discussionError(core.TrackerErrorUnavailable, "Jira comment contains unsupported non-text content")
	}
}

func approvedDiscussionBody(body string) adfNode {
	parts := strings.Split(body, "\n")
	content := make([]adfNode, 0, len(parts)*2)
	for i, part := range parts {
		if i > 0 {
			content = append(content, adfNode{Type: "hardBreak"})
		}
		if part != "" {
			content = append(content, adfNode{Type: "text", Text: part})
		}
	}
	return adfNode{Type: "doc", Version: 1, Content: []adfNode{{Type: "paragraph", Content: content}}}
}
