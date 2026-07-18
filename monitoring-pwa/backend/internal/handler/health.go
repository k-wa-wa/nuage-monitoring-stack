package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Health はサーバーの生存確認用エンドポイントである。
func Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
