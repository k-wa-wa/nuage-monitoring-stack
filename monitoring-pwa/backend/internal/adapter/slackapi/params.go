package slackapi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

func extractBearerToken(c echo.Context) string {
	authz := strings.TrimSpace(c.Request().Header.Get("Authorization"))
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}

// parseSlackParams は JSON ボディ・Form・Query のいずれの形式で送られたパラメータも統一的に読み取る。
func parseSlackParams(c echo.Context) map[string]interface{} {
	params := make(map[string]interface{})

	for k, vals := range c.QueryParams() {
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	ct := strings.ToLower(c.Request().Header.Get(echo.HeaderContentType))
	if strings.Contains(ct, "application/json") {
		var body map[string]interface{}
		if err := json.NewDecoder(c.Request().Body).Decode(&body); err == nil {
			for k, v := range body {
				params[k] = v
			}
		}
		return params
	}

	if form, err := c.FormParams(); err == nil {
		for k, vals := range form {
			if len(vals) > 0 {
				params[k] = vals[0]
			}
		}
	}
	return params
}

func paramString(params map[string]interface{}, key string) string {
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", s)
	}
}

func paramInt(params map[string]interface{}, key string, def int) int {
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if parsed, err := strconv.Atoi(n); err == nil {
			return parsed
		}
	}
	return def
}

func paramBool(params map[string]interface{}, key string) bool {
	v, ok := params[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	}
	return false
}
