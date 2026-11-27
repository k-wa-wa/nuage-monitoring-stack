package slackapi

import (
	"encoding/json"
	"errors"
	"strings"
)

func extractMessageContent(params map[string]interface{}) (string, string, string) {
	text := paramString(params, "text")
	blocksJSON, err := extractBlocksJSON(params["blocks"])
	if err != nil {
		return "", "", "invalid_blocks"
	}
	hasBlocks := blocksJSON != "" && blocksJSON != "[]"
	if strings.TrimSpace(text) == "" && !hasBlocks {
		return "", "", "no_text"
	}
	if blocksJSON == "[]" {
		blocksJSON = ""
	}
	if strings.TrimSpace(text) == "" && blocksJSON != "" {
		text = extractTextFromBlocks(blocksJSON)
	}
	return text, blocksJSON, ""
}

func validateBlocksArrayJSON(raw string) error {
	if !json.Valid([]byte(raw)) {
		return errors.New("invalid json")
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		return err
	}
	if blocks == nil {
		return errors.New("blocks must be a JSON array")
	}
	for _, b := range blocks {
		if b == nil {
			return errors.New("block element must be an object")
		}
		blockType, ok := b["type"].(string)
		if !ok || strings.TrimSpace(blockType) == "" {
			return errors.New("block type must be a non-empty string")
		}
		if rawText, exists := b["text"]; exists {
			if err := validateSlackTextObject(rawText); err != nil {
				return err
			}
		}
		if rawFields, exists := b["fields"]; exists {
			arr, ok := rawFields.([]interface{})
			if !ok {
				return errors.New("fields must be an array")
			}
			for _, item := range arr {
				if err := validateSlackTextObject(item); err != nil {
					return err
				}
			}
		}
		if rawElems, exists := b["elements"]; exists {
			arr, ok := rawElems.([]interface{})
			if !ok {
				return errors.New("elements must be an array")
			}
			for _, item := range arr {
				obj, ok := item.(map[string]interface{})
				if !ok || obj == nil {
					return errors.New("element must be an object")
				}
				elType, ok := obj["type"].(string)
				if !ok || strings.TrimSpace(elType) == "" {
					return errors.New("element type must be a non-empty string")
				}
				if elType == "mrkdwn" || elType == "plain_text" {
					if _, ok := obj["text"].(string); !ok {
						return errors.New("text element requires string text")
					}
				}
			}
		}
	}
	return nil
}

func validateSlackTextObject(v interface{}) error {
	obj, ok := v.(map[string]interface{})
	if !ok || obj == nil {
		return errors.New("text object must be an object")
	}
	t, ok := obj["type"].(string)
	if !ok || strings.TrimSpace(t) == "" {
		return errors.New("text object type must be a string")
	}
	if _, ok := obj["text"].(string); !ok {
		return errors.New("text object text must be a string")
	}
	return nil
}

func extractBlocksJSON(raw interface{}) (string, error) {
	if raw == nil {
		return "", nil
	}
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == "null" {
			return "", nil
		}
		if err := validateBlocksArrayJSON(trimmed); err != nil {
			return "", err
		}
		return trimmed, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		if string(b) == "null" {
			return "", nil
		}
		if err := validateBlocksArrayJSON(string(b)); err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func extractTextFromBlocks(blocksJSON string) string {
	var blocks []struct {
		Text *struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != nil && b.Text.Text != "" {
			parts = append(parts, b.Text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
