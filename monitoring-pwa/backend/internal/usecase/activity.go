package usecase

import (
	"fmt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// ActivityResult は通知タブ一覧の応答データである。
type ActivityResult struct {
	Items       []domain.ActivityItem
	HasMore     bool
	FiringCount int
}

// ActivityUsecase は通知タブ向けの履歴取得を扱う。
type ActivityUsecase struct {
	messages MessageRepository
}

// NewActivityUsecase は新しい ActivityUsecase を生成する。
func NewActivityUsecase(messages MessageRepository) *ActivityUsecase {
	return &ActivityUsecase{messages: messages}
}

// List はフィルタ・ページネーションに従って通知一覧と発生中件数を返す。
func (a *ActivityUsecase) List(filter domain.ActivityFilter, beforeTS string, limit int) (ActivityResult, error) {
	items, hasMore, err := a.messages.ListActivity(filter, beforeTS, limit)
	if err != nil {
		return ActivityResult{}, err
	}
	firing, err := a.messages.CountFiringAlerts()
	if err != nil {
		return ActivityResult{}, fmt.Errorf("%w: %v", ErrCountFiringAlerts, err)
	}
	return ActivityResult{
		Items:       items,
		HasMore:     hasMore,
		FiringCount: firing,
	}, nil
}
