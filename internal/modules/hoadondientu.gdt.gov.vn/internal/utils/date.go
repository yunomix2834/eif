package utils

import (
	"fmt"
	"time"
)

const inputDateLayout = "2006-01-02"
const hddtgdtDateLayout = "02/01/2006T15:04:05"

type DateRange struct {
	From time.Time
	To   time.Time
}

func ParseDateRange(fromDate, toDate string) (
	time.Time,
	time.Time,
	error,
) {
	// 2026-08-01 -> 2026-08-01 00:00:00
	from, err := time.ParseInLocation(
		inputDateLayout,
		fromDate,
		time.Local,
	)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"invalid from_date: %w",
			err,
		)
	}

	toDateValue, err := time.ParseInLocation(
		inputDateLayout,
		toDate,
		time.Local,
	)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"invalid to_date: %w",
			err,
		)
	}

	// Chỉnh về cuối ngày để không mất nguyên 1 ngày
	to := time.Date(
		toDateValue.Year(),
		toDateValue.Month(),
		toDateValue.Day(),
		23,
		59,
		59,
		0,
		toDateValue.Location(),
	)
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"to_date must not be before from_date",
		)
	}

	return from, to, nil
}

// CalculateInclusiveDays tính số ngày bao gồm cả ngày đầu và ngày cuối.
// Dùng UTC để không bị ảnh hưởng bởi timezone hoặc daylight saving time.
func CalculateInclusiveDays(from, to time.Time) int {
	start := time.Date(
		from.Year(),
		from.Month(),
		from.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	end := time.Date(
		to.Year(),
		to.Month(),
		to.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)

	return int(end.Sub(start).Hours()/24) + 1
}

func SplitDateRangeDescending(
	from, to time.Time,
	maxDays int,
) []DateRange {
	if maxDays <= 0 {
		return []DateRange{{
			From: from,
			To:   to,
		}}
	}

	var ranges []DateRange

	endDay := time.Date(
		to.Year(),
		to.Month(),
		to.Day(),
		23,
		59,
		59,
		0,
		to.Location(),
	)

	fromDay := time.Date(
		from.Year(),
		from.Month(),
		from.Day(),
		0,
		0,
		0,
		0,
		from.Location(),
	)

	// Loop từ ngày mới nhất về ngày cũ
	// HDDT GDT giới hạn khoảng tìm kiếm không được lớn hơn 1 tháng.
	// Không chỉ giới hạn theo số ngày vì tháng 2 có ít ngày hơn.
	// Vì vậy mỗi chunk không được vượt qua ranh giới tháng lịch.
	for !endDay.Before(fromDay) {
		start := time.Date(
			endDay.Year(),
			endDay.Month(),
			endDay.Day(),
			0,
			0,
			0,
			0,
			endDay.Location(),
		).AddDate(
			0,
			0,
			-(maxDays - 1),
		)

		monthStart := time.Date(
			endDay.Year(),
			endDay.Month(),
			1,
			0,
			0,
			0,
			0,
			endDay.Location(),
		)

		if start.Before(monthStart) {
			start = monthStart
		}

		if start.Before(fromDay) {
			start = fromDay
		}

		ranges = append(
			ranges,
			DateRange{
				From: start,
				To:   endDay,
			},
		)

		endDay = start.Add(-time.Second)
	}
	return ranges
}

func FormatHDDTGDTDate(t time.Time) string {
	return t.Format(hddtgdtDateLayout)
}

func FormatInputDate(t time.Time) string {
	return t.Format(inputDateLayout)
}
