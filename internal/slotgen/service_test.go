package slotgen

import (
	"testing"
	"time"
)

// TestHitungSlot_TepatEnam: aturan Senin 09:00-12:00 slot 30 menit
// -> 6 slot (09:00, 09:30, 10:00, 10:30, 11:00, 11:30).
// Start_at pertama harus tepat jam 09:00 waktu Asia/Jakarta.
func TestHitungSlot_TepatEnam(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	now := time.Now()
	daysUntilMonday := (8 - int(now.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	monday := time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 0, 0, 0, 0, campusTZ)

	from := monday
	to := monday

	rules := []ruleRecord{
		{
			dayOfWeek:       1,                                         // Senin
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),  // 09:00
			endTime:         time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC), // 12:00
			slotDurationMin: 30,
			effectiveFrom:   monday,
			effectiveTo:     nil,
		},
	}

	// now = sedikit sebelum Senin berikutnya, supaya slot tidak dianggap masa lalu
	futureNow := nextMonday.Add(-1 * time.Hour)
	slots := svc.computeShouldExist(from, to, rules, nil, futureNow.UTC())

	if len(slots) != 6 {
		t.Fatalf("jumlah slot = %d, mau 6", len(slots))
	}

	// Cek slot pertama: 09:00 Asia/Jakarta
	expectedFirstLocal := time.Date(monday.Year(), monday.Month(), monday.Day(), 9, 0, 0, 0, campusTZ)
	expectedFirst := expectedFirstLocal.UTC()
	if slots[0].startAt != expectedFirst {
		t.Errorf("slot pertama = %v, mau %v", slots[0].startAt, expectedFirst)
	}

	// Cek slot terakhir: 11:30 Asia/Jakarta
	expectedLastLocal := time.Date(monday.Year(), monday.Month(), monday.Day(), 11, 30, 0, 0, campusTZ)
	expectedLast := expectedLastLocal.UTC()
	if slots[5].startAt != expectedLast {
		t.Errorf("slot terakhir = %v, mau %v", slots[5].startAt, expectedLast)
	}
}

// TestHitungSlot_JendelaTakPenuh: jendela 09:00-10:20 slot 30 menit
// -> 2 slot (09:00, 09:30), sisa 20 menit tidak cukup untuk slot 30 menit.
func TestHitungSlot_JendelaTakPenuh(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	now := time.Now()
	daysUntilMonday := (8 - int(now.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	monday := time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 0, 0, 0, 0, campusTZ)
	from := monday
	to := monday

	rules := []ruleRecord{
		{
			dayOfWeek:       1,
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 10, 20, 0, 0, time.UTC), // 10:20, tidak cukup untuk slot 30 menit
			slotDurationMin: 30,
			effectiveFrom:   monday,
			effectiveTo:     nil,
		},
	}

	futureNow := nextMonday.Add(-1 * time.Hour)
	slots := svc.computeShouldExist(from, to, rules, nil, futureNow.UTC())

	if len(slots) != 2 {
		t.Fatalf("jumlah slot = %d, mau 2", len(slots))
	}
}

// TestHitungSlot_PengecualianFullDay: pengecualian full day -> tidak ada slot.
func TestHitungSlot_PengecualianFullDay(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	now := time.Now()
	daysUntilMonday := (8 - int(now.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	monday := time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 0, 0, 0, 0, campusTZ)
	from := monday
	to := monday

	rules := []ruleRecord{
		{
			dayOfWeek:       1,
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			slotDurationMin: 30,
			effectiveFrom:   monday,
			effectiveTo:     nil,
		},
	}

	// Pengecualian full day di tanggal yang sama
	exceptions := []exceptionRecord{
		{
			date:      monday,
			isFullDay: true,
		},
	}

	// now = 1 jam sebelum Senin berikutnya (slot belum lewat)
	futureNow := nextMonday.Add(-1 * time.Hour).UTC()
	slots := svc.computeShouldExist(from, to, rules, exceptions, futureNow)

	if len(slots) != 0 {
		t.Fatalf("jumlah slot = %d, mau 0 (diblokir full day)", len(slots))
	}
}

// TestHitungSlot_PengecualianParsial: pengecualian 10:00-11:00
// -> slot 09:00, 09:30, 11:00, 11:30 (slot yang beririsan dibuang).
func TestHitungSlot_PengecualianParsial(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	now := time.Now()
	daysUntilMonday := (8 - int(now.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	monday := time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 0, 0, 0, 0, campusTZ)

	// now = 1 jam sebelum Senin berikutnya (slot belum lewat)
	futureNow := nextMonday.Add(-1 * time.Hour).UTC()

	from := monday
	to := monday

	rules := []ruleRecord{
		{
			dayOfWeek:       1,
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			slotDurationMin: 30,
			effectiveFrom:   monday,
			effectiveTo:     nil,
		},
	}

	start10 := time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC)
	end11 := time.Date(0, 1, 1, 11, 0, 0, 0, time.UTC)
	exceptions := []exceptionRecord{
		{
			date:      monday,
			isFullDay: false,
			startTime: &start10,
			endTime:   &end11,
		},
	}

	slots := svc.computeShouldExist(from, to, rules, exceptions, futureNow)

	// Expect 4 slots: 09:00, 09:30, 11:00, 11:30
	if len(slots) != 4 {
		t.Fatalf("jumlah slot = %d, mau 4 (slot 10:00, 10:30 dibuang)", len(slots))
	}
}

// TestJendelaDefault_ZonaKampus: from/to harus di zona kampus, bukan UTC.
func TestJendelaDefault_ZonaKampus(t *testing.T) {
	// Simulasi server di UTC, tapi kampus di Asia/Jakarta (UTC+7).
	// Kalau tidak dikonversi, midnight UTC = 07:00 WIB, Geser 1 hari mundur.
	campusTZ, _ := time.LoadLocation("Asia/Jakarta")

	// Sekarang UTC: "2026-09-10 02:00:00 UTC" = "2026-09-10 09:00:00 WIB"
	nowUTC := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)

	from, to := JendelaDefault(nowUTC, campusTZ, 30)

	// Kalau implementasi benar, from = "2026-09-10 00:00:00 WIB"
	// Kalau implementasi salah (langsung Year/Month/Day dari UTC),
	// from = "2026-09-10 00:00:00 UTC" = "2026-09-10 07:00:00 WIB"
	expectedFrom := time.Date(2026, 9, 10, 0, 0, 0, 0, campusTZ)

	if !from.Equal(expectedFrom) {
		t.Errorf("from = %v, mau %v (zona kampus)", from, expectedFrom)
	}

	// to harus 30 hari setelah from
	expectedTo := expectedFrom.AddDate(0, 0, 30)
	if !to.Equal(expectedTo) {
		t.Errorf("to = %v, mau %v", to, expectedTo)
	}
}

// TestHitungSlot_DenganNow: computeShouldExist menerima parameter now.
// Slot yang start_at <= now tidak boleh ada di hasil.
func TestHitungSlot_DenganNow(t *testing.T) {
	// Zona kampus eksplisit (WIB = UTC+7)
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	// Hardcode Senin 14 September 2026
	// WIB = UTC+7, jadi jam 10:45 WIB = jam 03:45 UTC
	monday := time.Date(2026, 9, 14, 0, 0, 0, 0, campusTZ)         // Senin 00:00 WIB
	nowUTC := time.Date(2026, 9, 14, 10, 45, 0, 0, campusTZ).UTC() // 10:45 WIB = 03:45 UTC

	// Slot 11:30 WIB = 04:30 UTC (akan ada di hasil)

	from := monday
	to := monday

	rules := []ruleRecord{
		{
			dayOfWeek:       1, // Senin
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			slotDurationMin: 30,
			effectiveFrom:   monday,
			effectiveTo:     nil,
		},
	}

	slots := svc.computeShouldExist(from, to, rules, nil, nowUTC)

	// Expect 2 slots: 11:00 dan 11:30 (yang 09:00-10:30 sudah lewat)
	if len(slots) != 2 {
		t.Fatalf("jumlah slot = %d, mau 2 (slot 09:00-10:30 sudah lewat)", len(slots))
	}

	// Cek slot pertama adalah 11:00 UTC
	expectedFirst := time.Date(2026, 9, 14, 11, 0, 0, 0, campusTZ).UTC()
	if len(slots) > 0 && !slots[0].startAt.Equal(expectedFirst) {
		t.Errorf("slot[0] start = %v, mau %v", slots[0].startAt, expectedFirst)
	}

	// Semua slot harus > nowUTC
	for i, slot := range slots {
		if !slot.startAt.After(nowUTC) {
			t.Errorf("slot[%d] start=%v <= now=%v", i, slot.startAt, nowUTC)
		}
	}
}

// TestHitungSlot_TakAdaSlotMasaLampau: slot di masa lalu tidak boleh ada.
func TestHitungSlot_TakAdaSlotMasaLampau(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	// Tanggal kemarin jam 12 siang (pasti sebelum semua slot 09:00-12:00)
	yesterday := time.Now().AddDate(0, 0, -1).Add(12 * time.Hour)
	monday := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, campusTZ)

	from := monday
	to := monday

	rules := []ruleRecord{
		{
			dayOfWeek:       int(monday.Weekday()),
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			slotDurationMin: 30,
			effectiveFrom:   monday,
			effectiveTo:     nil,
		},
	}

	slots := svc.computeShouldExist(from, to, rules, nil, yesterday.UTC())

	if len(slots) != 0 {
		t.Fatalf("jumlah slot = %d, mau 0 (semuanya di masa lalu)", len(slots))
	}
}

// TestPengecualian_TakAdaPengecualian: tanpa pengecualian, tidak terblokir.
func TestPengecualian_TakAdaPengecualian(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	d := time.Date(2025, 10, 6, 0, 0, 0, 0, campusTZ)
	start := time.Date(2025, 10, 6, 9, 0, 0, 0, campusTZ)
	end := time.Date(2025, 10, 6, 9, 30, 0, 0, campusTZ)

	result := terblokirOlehPengecualian(d, start, end, nil, campusTZ)
	if result {
		t.Error("seharusnya tidak terblokir tanpa pengecualian")
	}
}

// TestPengecualian_FullDay: pengecualian full day memblokir semua slot.
func TestPengecualian_FullDay(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	d := time.Date(2025, 10, 6, 0, 0, 0, 0, campusTZ)

	exceptions := []exceptionRecord{
		{date: d, isFullDay: true},
	}

	start := time.Date(2025, 10, 6, 9, 0, 0, 0, campusTZ)
	end := time.Date(2025, 10, 6, 9, 30, 0, 0, campusTZ)
	result := terblokirOlehPengecualian(d, start, end, exceptions, campusTZ)
	if !result {
		t.Error("seharusnya terblokir karena pengecualian full day")
	}
}

// TestPengecualian_IrisanParsial: slot 09:00-09:30 beririsan dengan 09:00-10:00.
func TestPengecualian_IrisanParsial(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	d := time.Date(2025, 10, 6, 0, 0, 0, 0, campusTZ)

	start10 := time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)
	end10 := time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC)
	exceptions := []exceptionRecord{
		{
			date:      d,
			isFullDay: false,
			startTime: &start10,
			endTime:   &end10,
		},
	}

	start := time.Date(2025, 10, 6, 9, 0, 0, 0, campusTZ)
	end := time.Date(2025, 10, 6, 9, 30, 0, 0, campusTZ)
	result := terblokirOlehPengecualian(d, start, end, exceptions, campusTZ)
	if !result {
		t.Error("seharusnya terblokir karena irisan dengan pengecualian")
	}
}

// TestPengecualian_TakBeririsan: slot 11:00-11:30 tidak beririsan dengan 09:00-10:00.
func TestPengecualian_TakBeririsan(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	d := time.Date(2025, 10, 6, 0, 0, 0, 0, campusTZ)

	start10 := time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)
	end10 := time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC)
	exceptions := []exceptionRecord{
		{
			date:      d,
			isFullDay: false,
			startTime: &start10,
			endTime:   &end10,
		},
	}

	start := time.Date(2025, 10, 6, 11, 0, 0, 0, campusTZ)
	end := time.Date(2025, 10, 6, 11, 30, 0, 0, campusTZ)
	result := terblokirOlehPengecualian(d, start, end, exceptions, campusTZ)
	if result {
		t.Error("seharusnya tidak terblokir karena tidak beririsan")
	}
}

// TestPengecualian_SesudahPengecualian: slot 10:00-10:30 tepat setelah pengecualian 09:00-10:00.
// Karena pengecualian pakai [start, end) (end exclusive), 10:00 tidak beririsan.
func TestPengecualian_SesudahPengecualian(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	d := time.Date(2025, 10, 6, 0, 0, 0, 0, campusTZ)

	start10 := time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)
	end10 := time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC)
	exceptions := []exceptionRecord{
		{
			date:      d,
			isFullDay: false,
			startTime: &start10,
			endTime:   &end10,
		},
	}

	start := time.Date(2025, 10, 6, 10, 0, 0, 0, campusTZ)
	end := time.Date(2025, 10, 6, 10, 30, 0, 0, campusTZ)
	result := terblokirOlehPengecualian(d, start, end, exceptions, campusTZ)
	if result {
		t.Error("seharusnya tidak terblokir karena end exclusive 10:00")
	}
}

// TestPengecualian_IrisanSempadan: slot 09:30-10:00 beririsan dengan pengecualian 09:00-10:00.
func TestPengecualian_IrisanSempadan(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	d := time.Date(2025, 10, 6, 0, 0, 0, 0, campusTZ)

	start10 := time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)
	end10 := time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC)
	exceptions := []exceptionRecord{
		{
			date:      d,
			isFullDay: false,
			startTime: &start10,
			endTime:   &end10,
		},
	}

	start := time.Date(2025, 10, 6, 9, 30, 0, 0, campusTZ)
	end := time.Date(2025, 10, 6, 10, 0, 0, 0, campusTZ)
	result := terblokirOlehPengecualian(d, start, end, exceptions, campusTZ)
	if !result {
		t.Error("seharusnya terblokir karena beririsan di 09:30-10:00")
	}
}

// TestHitungSlot_DuaMinggu: aturan selama 2 minggu.
func TestHitungSlot_DuaMinggu(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	now := time.Now()
	daysUntilMonday := (8 - int(now.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	monday1 := time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 0, 0, 0, 0, campusTZ)
	monday2 := monday1.AddDate(0, 0, 7)

	from := monday1
	to := monday2

	rules := []ruleRecord{
		{
			dayOfWeek:       1, // Senin
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			slotDurationMin: 30,
			effectiveFrom:   monday1,
			effectiveTo:     nil,
		},
	}

	futureNow := monday1.Add(-1 * time.Hour)
	slots := svc.computeShouldExist(from, to, rules, nil, futureNow.UTC())

	// Expect 12 slots: 6 di Senin pertama + 6 di Senin kedua
	if len(slots) != 12 {
		t.Fatalf("jumlah slot = %d, mau 12", len(slots))
	}
}

// TestHitungSlot_TakMelampauiHorizon: slot tidak boleh melampaui horizon.
func TestHitungSlot_TakMelampauiHorizon(t *testing.T) {
	campusTZ, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	svc := &Service{
		campusTZ:    campusTZ,
		horizonDays: 30,
	}

	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, campusTZ)
	to := from.AddDate(0, 0, 30)

	// Aturan Senin jam 9-10, 30 menit
	rules := []ruleRecord{
		{
			dayOfWeek:       1, // Senin
			startTime:       time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
			endTime:         time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC),
			slotDurationMin: 30,
			effectiveFrom:   from,
			effectiveTo:     nil,
		},
	}

	slots := svc.computeShouldExist(from, to, rules, nil, now.UTC())

	horizonLimit := to.UTC()
	for _, slot := range slots {
		if slot.startAt.After(horizonLimit) {
			t.Errorf("slot melampaui horizon: start=%v, limit=%v", slot.startAt, horizonLimit)
		}
	}
}
