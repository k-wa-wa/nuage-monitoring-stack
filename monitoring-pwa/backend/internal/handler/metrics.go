package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

// MetricsHandler は Prometheus からクラスタ状況をクエリするハンドラである。
type MetricsHandler struct {
	prometheusURL string
}

func NewMetricsHandler(prometheusURL string) *MetricsHandler {
	return &MetricsHandler{prometheusURL: prometheusURL}
}

// ClusterStatus はフロントエンドに返却するクラスタステータスのレスポンス構造体である。
type ClusterStatus struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	DiskPercent   float64   `json:"disk_percent"`
	NodesReady    int       `json:"nodes_ready"`
	NodesTotal    int       `json:"nodes_total"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (h *MetricsHandler) queryPrometheus(query string) (float64, error) {
	u, err := url.Parse(h.prometheusURL + "/api/v1/query")
	if err != nil {
		return 0, err
	}

	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var pr prometheusResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return 0, err
	}

	if pr.Status != "success" || len(pr.Data.Result) == 0 {
		return 0, fmt.Errorf("query return empty or failed status")
	}

	valArr := pr.Data.Result[0].Value
	if len(valArr) < 2 {
		return 0, fmt.Errorf("invalid value array length")
	}

	valStr, ok := valArr[1].(string)
	if !ok {
		return 0, fmt.Errorf("value is not a string")
	}

	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, err
	}

	return val, nil
}

// ClusterStatus は Prometheus から現在のクラスタ状況を取得して返す。
func (h *MetricsHandler) ClusterStatus(c echo.Context) error {
	status := ClusterStatus{
		UpdatedAt: time.Now(),
	}

	// 1. CPU使用率
	cpuQuery := `100 - (avg(irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`
	if val, err := h.queryPrometheus(cpuQuery); err == nil {
		status.CPUPercent = val
	} else {
		slog.Warn("failed to fetch cpu usage from prometheus", "error", err)
	}

	// 2. メモリ使用率
	memQuery := `100 * (1 - sum(node_memory_MemAvailable_bytes) / sum(node_memory_MemTotal_bytes))`
	if val, err := h.queryPrometheus(memQuery); err == nil {
		status.MemoryPercent = val
	} else {
		slog.Warn("failed to fetch memory usage from prometheus", "error", err)
	}

	// 3. ディスク使用率
	diskQuery := `100 * (sum(node_filesystem_size_bytes{mountpoint="/"}) - sum(node_filesystem_free_bytes{mountpoint="/"})) / sum(node_filesystem_size_bytes{mountpoint="/"})`
	if val, err := h.queryPrometheus(diskQuery); err == nil {
		status.DiskPercent = val
	} else {
		slog.Warn("failed to fetch disk usage from prometheus", "error", err)
	}

	// 4. Nodes Ready 数
	nodesReadyQuery := `sum(kube_node_status_condition{condition="Ready", status="true"})`
	if val, err := h.queryPrometheus(nodesReadyQuery); err == nil {
		status.NodesReady = int(val)
	} else {
		slog.Warn("failed to fetch ready nodes count from prometheus", "error", err)
	}

	// 5. 総 Nodes 数
	nodesTotalQuery := `count(kube_node_info)`
	if val, err := h.queryPrometheus(nodesTotalQuery); err == nil {
		status.NodesTotal = int(val)
	} else {
		slog.Warn("failed to fetch total nodes count from prometheus", "error", err)
	}

	// すべての値が取得に失敗した場合は500を返さず、取得できた限りの情報、またはデフォルト値でレスポンスする。
	return c.JSON(http.StatusOK, status)
}
