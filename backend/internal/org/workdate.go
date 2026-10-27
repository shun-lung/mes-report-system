package org

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ShiftRule struct {
	ID             uuid.UUID
	Start          time.Duration // 距當地午夜
	WorkDateOffset int           // 0 或 -1
}

// ResolveWorkDate 以「最近一個已開始的班別」決定班別與生產日：
// 跨午夜的夜班歸屬開班當日；班與班之間的空檔（加班）歸屬前一班；
// 00:00 起的大夜班若要算前一天，設定 WorkDateOffset = -1。
func ResolveWorkDate(rules []ShiftRule, t time.Time, loc *time.Location) (uuid.UUID, time.Time, error) {
	if len(rules) == 0 {
		return uuid.Nil, time.Time{}, fmt.Errorf("工廠尚未設定班別")
	}
	lt := t.In(loc)
	today := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC)
	tod := time.Duration(lt.Hour())*time.Hour + time.Duration(lt.Minute())*time.Minute +
		time.Duration(lt.Second())*time.Second + time.Duration(lt.Nanosecond())

	var best ShiftRule
	var bestDate time.Time
	bestElapsed := time.Duration(-1)
	for _, r := range rules {
		elapsed, startDate := tod-r.Start, today
		if elapsed < 0 {
			elapsed += 24 * time.Hour
			startDate = today.AddDate(0, 0, -1)
		}
		if bestElapsed < 0 || elapsed < bestElapsed {
			best, bestDate, bestElapsed = r, startDate, elapsed
		}
	}
	return best.ID, bestDate.AddDate(0, 0, best.WorkDateOffset), nil
}

// ParseClock 解析 "HH:MM" 或 "HH:MM:SS"。
func ParseClock(s string) (time.Duration, error) {
	for _, layout := range []string{"15:04", "15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second, nil
		}
	}
	return 0, fmt.Errorf("時間格式錯誤：%s", s)
}
