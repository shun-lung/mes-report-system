package org

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestResolveWorkDate(t *testing.T) {
	taipei, _ := time.LoadLocation("Asia/Taipei")
	day, night := uuid.New(), uuid.New()
	twoShifts := []ShiftRule{
		{ID: day, Start: 8 * time.Hour},
		{ID: night, Start: 20 * time.Hour}, // 20:00 ~ 08:00 跨午夜
	}
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	threeShifts := []ShiftRule{
		{ID: a, Start: 8 * time.Hour},
		{ID: b, Start: 16 * time.Hour},
		{ID: c, Start: 0, WorkDateOffset: -1}, // 00:00 ~ 08:00 大夜算前一天
	}

	tests := []struct {
		name      string
		rules     []ShiftRule
		local     string
		wantShift uuid.UUID
		wantDate  string
	}{
		{"日班", twoShifts, "2026-10-27 10:00", day, "2026-10-27"},
		{"夜班午夜前", twoShifts, "2026-10-27 23:30", night, "2026-10-27"},
		{"夜班跨午夜歸開班日", twoShifts, "2026-10-28 03:00", night, "2026-10-27"},
		{"日班開班整點", twoShifts, "2026-10-28 08:00", day, "2026-10-28"},
		{"跨月夜班", twoShifts, "2026-11-01 02:00", night, "2026-10-31"},
		{"三班中班", threeShifts, "2026-10-27 17:00", b, "2026-10-27"},
		{"三班大夜算前一天", threeShifts, "2026-10-28 01:00", c, "2026-10-27"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lt, err := time.ParseInLocation("2006-01-02 15:04", tt.local, taipei)
			if err != nil {
				t.Fatal(err)
			}
			// 以 UTC 傳入，確認時區轉換正確
			shift, date, err := ResolveWorkDate(tt.rules, lt.UTC(), taipei)
			if err != nil {
				t.Fatal(err)
			}
			if shift != tt.wantShift || date.Format("2006-01-02") != tt.wantDate {
				t.Errorf("got shift=%v date=%s, want shift=%v date=%s", shift, date.Format("2006-01-02"), tt.wantShift, tt.wantDate)
			}
		})
	}

	if _, _, err := ResolveWorkDate(nil, time.Now(), taipei); err == nil {
		t.Error("expected error when no shifts")
	}
}
