package availability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound dikembalikan saat aturan/pengecualian tidak ditemukan.
// Dipakai untuk menghasilkan 404, bukan 403 — 403 mengonfirmasi keberadaan.
var ErrNotFound = errors.New("tidak ditemukan")

// ErrRuleConflict dikembalikan saat aturan baru bertabrakan dengan yang ada.
var ErrRuleConflict = errors.New("aturan bentrok")

// ErrInvalidTimeOrder dikembalikan saat end_time <= start_time.
var ErrInvalidTimeOrder = errors.New("waktu selesai harus setelah waktu mulai")

// ErrWindowTooSmall dikembalikan saat jendela waktu lebih pendek dari durasi slot.
var ErrWindowTooSmall = errors.New("jendela waktu lebih pendek dari durasi slot")

// ErrInvalidDateOrder dikembalikan saat effective_to < effective_from.
var ErrInvalidDateOrder = errors.New("tanggal selesai harus setelah atau sama dengan tanggal mulai")

// ErrInvalidException dikembalikan saat exception tidak memiliki jam yang valid.
var ErrInvalidException = errors.New("pengecualian parsial harus memiliki jam mulai dan selesai")

// Service menangani aturan ketersediaan dosen.
type Service struct {
	pool *pgxpool.Pool
}

// NewService membuat availability service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ---------------------------------------------------------------- rules

// CreateRuleInput adalah input untuk membuat aturan ketersediaan.
type CreateRuleInput struct {
	LecturerID      string
	DayOfWeek       int    // 0=Minggu, 6=Sabtu
	StartTime       string // HH:MM
	EndTime         string // HH:MM
	SlotDurationMin int
	EffectiveFrom   string // YYYY-MM-DD
	EffectiveTo     string // opsional, YYYY-MM-DD
}

// CreateRule membuat aturan ketersediaan baru.
func (s *Service) CreateRule(ctx context.Context, input CreateRuleInput) (string, error) {
	startT, err := time.Parse("15:04", input.StartTime)
	if err != nil {
		return "", fmt.Errorf("start_time tidak valid: %w", err)
	}
	endT, err := time.Parse("15:04", input.EndTime)
	if err != nil {
		return "", fmt.Errorf("end_time tidak valid: %w", err)
	}
	if !endT.After(startT) {
		return "", ErrInvalidTimeOrder
	}

	windowSeconds := endT.Sub(startT).Seconds()
	if windowSeconds < float64(input.SlotDurationMin*60) {
		return "", ErrWindowTooSmall
	}

	effectiveFrom, err := time.Parse("2006-01-02", input.EffectiveFrom)
	if err != nil {
		return "", fmt.Errorf("effective_from tidak valid: %w", err)
	}

	var effectiveTo *time.Time
	if input.EffectiveTo != "" {
		t, err := time.Parse("2006-01-02", input.EffectiveTo)
		if err != nil {
			return "", fmt.Errorf("effective_to tidak valid: %w", err)
		}
		if t.Before(effectiveFrom) {
			return "", ErrInvalidDateOrder
		}
		effectiveTo = &t
	}

	var id string
	var query string
	var args []any

	if effectiveTo != nil {
		query = `
			INSERT INTO availability_rules
			(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from, effective_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id::text`
		args = []any{input.LecturerID, input.DayOfWeek, startT, endT, input.SlotDurationMin, effectiveFrom, *effectiveTo}
	} else {
		query = `
			INSERT INTO availability_rules
			(lecturer_id, day_of_week, start_time, end_time, slot_duration_min, effective_from)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text`
		args = []any{input.LecturerID, input.DayOfWeek, startT, endT, input.SlotDurationMin, effectiveFrom}
	}

	err = s.pool.QueryRow(ctx, query, args...).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23P01": // exclusion_violation
				return "", ErrRuleConflict
			}
		}
		return "", fmt.Errorf("membuat aturan: %w", err)
	}

	return id, nil
}

// Rule adalah aturan ketersediaan.
type Rule struct {
	ID              string    `json:"id"`
	LecturerID      string    `json:"lecturer_id"`
	DayOfWeek       int       `json:"day_of_week"`
	StartTime       string    `json:"start_time"` // HH:MM
	EndTime         string    `json:"end_time"`   // HH:MM
	SlotDurationMin int       `json:"slot_duration_min"`
	EffectiveFrom   string    `json:"effective_from"`         // YYYY-MM-DD
	EffectiveTo     *string   `json:"effective_to,omitempty"` // YYYY-MM-DD
	IsActive        bool      `json:"is_active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// GetRulesByLecturer mengambil semua aturan aktif milik dosen.
func (s *Service) GetRulesByLecturer(ctx context.Context, lecturerID string) ([]Rule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, lecturer_id::text, day_of_week,
		       start_time::text, end_time::text,
		       slot_duration_min,
		       effective_from::text, effective_to::text,
		       is_active, created_at, updated_at
		FROM availability_rules
		WHERE lecturer_id = $1 AND is_active = true
		ORDER BY day_of_week, start_time`,
		lecturerID)
	if err != nil {
		return nil, fmt.Errorf("mengambil aturan: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		var startT, endT time.Time
		err := rows.Scan(
			&r.ID, &r.LecturerID, &r.DayOfWeek,
			&startT, &endT,
			&r.SlotDurationMin,
			&r.EffectiveFrom, &r.EffectiveTo,
			&r.IsActive, &r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("membaca baris aturan: %w", err)
		}
		r.StartTime = startT.Format("15:04")
		r.EndTime = endT.Format("15:04")
		rules = append(rules, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi aturan: %w", err)
	}

	return rules, nil
}

// GetRuleByID mengambil satu aturan berdasarkan ID dan memverifikasi kepemilikan.
func (s *Service) GetRuleByID(ctx context.Context, ruleID, lecturerID string) (*Rule, error) {
	var r Rule
	var startT, endT time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, lecturer_id::text, day_of_week,
		       start_time::text, end_time::text,
		       slot_duration_min,
		       effective_from::text, effective_to::text,
		       is_active, created_at, updated_at
		FROM availability_rules
		WHERE id = $1 AND lecturer_id = $2`,
		ruleID, lecturerID,
	).Scan(
		&r.ID, &r.LecturerID, &r.DayOfWeek,
		&startT, &endT,
		&r.SlotDurationMin,
		&r.EffectiveFrom, &r.EffectiveTo,
		&r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengambil aturan: %w", err)
	}
	r.StartTime = startT.Format("15:04")
	r.EndTime = endT.Format("15:04")
	return &r, nil
}

// UpdateRuleInput adalah input untuk mengubah aturan.
// Hanya effective_to dan is_active yang boleh diubah.
type UpdateRuleInput struct {
	EffectiveTo *string
	IsActive    *bool
}

// UpdateRule mengubah aturan. Hanya mengubah effective_to dan is_active.
func (s *Service) UpdateRule(ctx context.Context, ruleID, lecturerID string, input UpdateRuleInput) error {
	// Parse effective_to kalau ada
	var effectiveTo *time.Time
	if input.EffectiveTo != nil && *input.EffectiveTo != "" {
		t, err := time.Parse("2006-01-02", *input.EffectiveTo)
		if err != nil {
			return fmt.Errorf("effective_to tidak valid: %w", err)
		}
		effectiveTo = &t
	}

	var query string
	var args []any

	if input.IsActive != nil && effectiveTo != nil {
		query = `
			UPDATE availability_rules
			SET effective_to = $1, is_active = $2
			WHERE id = $3 AND lecturer_id = $4 AND is_active = true
			RETURNING id`
		args = []any{*effectiveTo, *input.IsActive, ruleID, lecturerID}
	} else if input.IsActive != nil {
		query = `
			UPDATE availability_rules
			SET is_active = $1
			WHERE id = $2 AND lecturer_id = $3 AND is_active = true
			RETURNING id`
		args = []any{*input.IsActive, ruleID, lecturerID}
	} else if effectiveTo != nil {
		query = `
			UPDATE availability_rules
			SET effective_to = $1
			WHERE id = $2 AND lecturer_id = $3 AND is_active = true
			RETURNING id`
		args = []any{*effectiveTo, ruleID, lecturerID}
	} else {
		return nil // tidak ada yang diubah
	}

	var id string
	err := s.pool.QueryRow(ctx, query, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("mengubah aturan: %w", err)
	}
	return nil
}

// DeleteRule melakukan soft delete (is_active = false).
func (s *Service) DeleteRule(ctx context.Context, ruleID, lecturerID string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE availability_rules
		SET is_active = false
		WHERE id = $1 AND lecturer_id = $2 AND is_active = true`,
		ruleID, lecturerID)
	if err != nil {
		return fmt.Errorf("menghapus aturan: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- exceptions

// CreateExceptionInput adalah input untuk membuat pengecualian.
type CreateExceptionInput struct {
	LecturerID    string
	ExceptionDate string // YYYY-MM-DD
	Reason        string
	IsFullDay     bool
	StartTime     string // HH:MM, kosong jika is_full_day
	EndTime       string // HH:MM, kosong jika is_full_day
}

// CreateException membuat pengecualian ketersediaan.
func (s *Service) CreateException(ctx context.Context, input CreateExceptionInput) (string, error) {
	exceptionDate, err := time.Parse("2006-01-02", input.ExceptionDate)
	if err != nil {
		return "", fmt.Errorf("exception_date tidak valid: %w", err)
	}

	var startT, endT interface{}
	if !input.IsFullDay {
		if input.StartTime == "" || input.EndTime == "" {
			return "", ErrInvalidException
		}
		st, err := time.Parse("15:04", input.StartTime)
		if err != nil {
			return "", fmt.Errorf("start_time tidak valid: %w", err)
		}
		et, err := time.Parse("15:04", input.EndTime)
		if err != nil {
			return "", fmt.Errorf("end_time tidak valid: %w", err)
		}
		if !et.After(st) {
			return "", ErrInvalidTimeOrder
		}
		startT = st
		endT = et
	}

	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO availability_exceptions
		(lecturer_id, exception_date, reason, is_full_day, start_time, end_time)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text`,
		input.LecturerID, exceptionDate, input.Reason, input.IsFullDay, startT, endT,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// CHECK constraint violation
			if pgErr.Code == "23514" {
				return "", ErrInvalidException
			}
		}
		return "", fmt.Errorf("membuat pengecualian: %w", err)
	}

	return id, nil
}

// Exception adalah pengecualian ketersediaan.
type Exception struct {
	ID            string    `json:"id"`
	LecturerID    string    `json:"lecturer_id"`
	ExceptionDate string    `json:"exception_date"` // YYYY-MM-DD
	Reason        string    `json:"reason"`
	IsFullDay     bool      `json:"is_full_day"`
	StartTime     *string   `json:"start_time,omitempty"` // HH:MM
	EndTime       *string   `json:"end_time,omitempty"`   // HH:MM
	CreatedAt     time.Time `json:"created_at"`
}

// GetExceptionsByLecturer mengambil pengecualian dalam rentang tanggal.
func (s *Service) GetExceptionsByLecturer(ctx context.Context, lecturerID string, from, to string) ([]Exception, error) {
	fromDate, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil, fmt.Errorf("from tidak valid: %w", err)
	}
	toDate, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("to tidak valid: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id::text, lecturer_id::text, exception_date::text,
		       reason, is_full_day,
		       start_time::text, end_time::text,
		       created_at
		FROM availability_exceptions
		WHERE lecturer_id = $1 AND exception_date BETWEEN $2 AND $3
		ORDER BY exception_date`,
		lecturerID, fromDate, toDate)
	if err != nil {
		return nil, fmt.Errorf("mengambil pengecualian: %w", err)
	}
	defer rows.Close()

	var exceptions []Exception
	for rows.Next() {
		var e Exception
		var startT, endT interface{}
		err := rows.Scan(
			&e.ID, &e.LecturerID, &e.ExceptionDate,
			&e.Reason, &e.IsFullDay,
			&startT, &endT,
			&e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("membaca baris pengecualian: %w", err)
		}
		if startT != nil {
			st := startT.(time.Time).Format("15:04")
			et := endT.(time.Time).Format("15:04")
			e.StartTime = &st
			e.EndTime = &et
		}
		exceptions = append(exceptions, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi pengecualian: %w", err)
	}

	return exceptions, nil
}

// GetExceptionByID mengambil satu pengecualian dan memverifikasi kepemilikan.
func (s *Service) GetExceptionByID(ctx context.Context, exceptionID, lecturerID string) (*Exception, error) {
	var e Exception
	var startT, endT interface{}
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, lecturer_id::text, exception_date::text,
		       reason, is_full_day,
		       start_time::text, end_time::text,
		       created_at
		FROM availability_exceptions
		WHERE id = $1 AND lecturer_id = $2`,
		exceptionID, lecturerID,
	).Scan(
		&e.ID, &e.LecturerID, &e.ExceptionDate,
		&e.Reason, &e.IsFullDay,
		&startT, &endT,
		&e.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengambil pengecualian: %w", err)
	}
	if startT != nil {
		st := startT.(time.Time).Format("15:04")
		et := endT.(time.Time).Format("15:04")
		e.StartTime = &st
		e.EndTime = &et
	}
	return &e, nil
}

// DeleteException menghapus pengecualian.
func (s *Service) DeleteException(ctx context.Context, exceptionID, lecturerID string) error {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM availability_exceptions
		WHERE id = $1 AND lecturer_id = $2`,
		exceptionID, lecturerID)
	if err != nil {
		return fmt.Errorf("menghapus pengecualian: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
