package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Kumpulan test untuk bug yang lolos dari suite lama karena jalurnya tidak
// pernah disentuh: list aturan yang berisi data, pengecualian sebagian hari,
// dan ID di path yang bukan UUID.

func TestAvailabilityRule_ListMengembalikanJamHHMM(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-rules", map[string]any{
		"day_of_week":       2,
		"start_time":        "09:00",
		"end_time":          "12:00",
		"slot_duration_min": 30,
		"effective_from":    "2026-09-14",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Test lama cuma me-list aturan milik dosen yang TIDAK punya aturan, jadi
	// kode pemindaian baris tidak pernah jalan dan 500-nya tidak ketahuan.
	rec = kirim(t, h, http.MethodGet, "/api/v1/availability-rules", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var hasil struct {
		Rules []struct {
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hasil); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(hasil.Rules) != 1 {
		t.Fatalf("jumlah aturan = %d, mau 1", len(hasil.Rules))
	}
	if hasil.Rules[0].StartTime != "09:00" || hasil.Rules[0].EndTime != "12:00" {
		t.Errorf("jam = %s-%s, mau 09:00-12:00", hasil.Rules[0].StartTime, hasil.Rules[0].EndTime)
	}
}

func TestAvailabilityException_ListSebagianHari(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodPost, "/api/v1/availability-exceptions", map[string]any{
		"exception_date": "2026-09-15",
		"reason":         "Rapat Senat",
		"is_full_day":    false,
		"start_time":     "13:00",
		"end_time":       "15:30",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = kirim(t, h, http.MethodGet, "/api/v1/availability-exceptions?from=2026-09-01&to=2026-09-30", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var hasil struct {
		Exceptions []struct {
			StartTime *string `json:"start_time"`
			EndTime   *string `json:"end_time"`
		} `json:"exceptions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hasil); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(hasil.Exceptions) != 1 {
		t.Fatalf("jumlah pengecualian = %d, mau 1", len(hasil.Exceptions))
	}
	e := hasil.Exceptions[0]
	if e.StartTime == nil || e.EndTime == nil || *e.StartTime != "13:00" || *e.EndTime != "15:30" {
		t.Errorf("jam pengecualian tidak sesuai: %v-%v", e.StartTime, e.EndTime)
	}
}

func TestAvailability_ListKosongBalasArrayBukanNull(t *testing.T) {
	h := serverLengkap(t)
	token, _ := loginDosen(t, h)

	rec := kirim(t, h, http.MethodGet, "/api/v1/availability-rules", nil, token)
	if got := rec.Body.String(); rec.Code != http.StatusOK || got != `{"rules":[]}`+"\n" {
		t.Errorf("list aturan kosong: status=%d body=%q", rec.Code, got)
	}
	rec = kirim(t, h, http.MethodGet, "/api/v1/availability-exceptions?from=2026-09-01&to=2026-09-30", nil, token)
	if got := rec.Body.String(); rec.Code != http.StatusOK || got != `{"exceptions":[]}`+"\n" {
		t.Errorf("list pengecualian kosong: status=%d body=%q", rec.Code, got)
	}
}

func TestIDBukanUUID_Balas404BukanError500(t *testing.T) {
	h := serverLengkap(t)
	tokenDosen, _ := loginDosen(t, h)
	tokenMhs, _ := loginLengkap(t, h)

	kasus := []struct {
		metode, path, token string
	}{
		{http.MethodGet, "/api/v1/lecturers/bukan-uuid", ""},
		{http.MethodGet, "/api/v1/lecturers/bukan-uuid/slots", ""},
		{http.MethodGet, "/api/v1/bookings/bukan-uuid", tokenMhs},
		{http.MethodPatch, "/api/v1/bookings/bukan-uuid/cancel", tokenMhs},
		{http.MethodPatch, "/api/v1/bookings/bukan-uuid/complete", tokenDosen},
		{http.MethodPatch, "/api/v1/bookings/bukan-uuid/no-show", tokenDosen},
		{http.MethodPatch, "/api/v1/availability-rules/bukan-uuid", tokenDosen},
		{http.MethodDelete, "/api/v1/availability-rules/bukan-uuid", tokenDosen},
		{http.MethodDelete, "/api/v1/availability-exceptions/bukan-uuid", tokenDosen},
	}
	for _, k := range kasus {
		t.Run(k.metode+" "+k.path, func(t *testing.T) {
			var body any
			if k.metode == http.MethodPatch {
				body = map[string]any{}
			}
			rec := kirim(t, h, k.metode, k.path, body, k.token)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status=%d body=%s, mau 404", rec.Code, rec.Body.String())
			}
		})
	}
}
