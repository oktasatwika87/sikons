package lecturer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("dosen tidak ditemukan")

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type Lecturer struct {
	ID         string `json:"id"`
	FullName   string `json:"full_name"`
	Department string `json:"department"`
	Room       string `json:"room"`
	Bio        string `json:"bio"`
}

type ListParams struct {
	Department string
	Q          string
	Page       int
	PerPage    int
}

type ListResult struct {
	Lecturers []Lecturer `json:"lecturers"`
	Total     int        `json:"total"`
	Page      int        `json:"page"`
	PerPage   int        `json:"per_page"`
}

func (s *Service) List(ctx context.Context, params ListParams) (ListResult, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PerPage < 1 {
		params.PerPage = 10
	}
	if params.PerPage > 100 {
		params.PerPage = 100
	}

	offset := (params.Page - 1) * params.PerPage

	// Bangun query dinamis
	query := `
		SELECT u.id::text, u.full_name, lp.department, COALESCE(lp.room, ''), COALESCE(lp.bio, '')
	FROM users u
	JOIN lecturer_profiles lp ON lp.user_id = u.id
	WHERE u.role = 'lecturer' AND u.is_active = true
	`
	countQuery := `
		SELECT count(*)
		FROM users u
		JOIN lecturer_profiles lp ON lp.user_id = u.id
		WHERE u.role = 'lecturer' AND u.is_active = true
	`
	var args []any
	argIdx := 1

	if params.Department != "" {
		query += fmt.Sprintf(" AND lp.department = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND lp.department = $%d", argIdx)
		args = append(args, params.Department)
		argIdx++
	}

	if params.Q != "" {
		query += fmt.Sprintf(" AND u.full_name ILIKE $%d", argIdx)
		countQuery += fmt.Sprintf(" AND u.full_name ILIKE $%d", argIdx)
		args = append(args, "%"+params.Q+"%")
		argIdx++
	}

	// Count total
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return ListResult{}, fmt.Errorf("menghitung dosen: %w", err)
	}

	// Ambil data dengan pagination
	query += fmt.Sprintf(" ORDER BY u.full_name LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, params.PerPage, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("mengambil daftar dosen: %w", err)
	}
	defer rows.Close()

	var lecturers []Lecturer
	for rows.Next() {
		var l Lecturer
		err := rows.Scan(&l.ID, &l.FullName, &l.Department, &l.Room, &l.Bio)
		if err != nil {
			return ListResult{}, fmt.Errorf("membaca baris dosen: %w", err)
		}
		lecturers = append(lecturers, l)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterasi dosen: %w", err)
	}

	return ListResult{
		Lecturers: lecturers,
		Total:     total,
		Page:      params.Page,
		PerPage:   params.PerPage,
	}, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*Lecturer, error) {
	var l Lecturer
	err := s.pool.QueryRow(ctx, `
		SELECT u.id::text, u.full_name, lp.department, COALESCE(lp.room, ''), COALESCE(lp.bio, '')
		FROM users u
		JOIN lecturer_profiles lp ON lp.user_id = u.id
		WHERE u.id = $1::uuid
		  AND u.role = 'lecturer'
		  AND u.is_active = true
	`, id).Scan(&l.ID, &l.FullName, &l.Department, &l.Room, &l.Bio)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

type Slot struct {
	ID      string    `json:"id"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
	Status  string    `json:"status"`
}

func (s *Service) GetSlots(ctx context.Context, lecturerID string, from, to time.Time) ([]Slot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, start_at, end_at, status::text
		FROM slots
		WHERE lecturer_id = $1::uuid
		  AND start_at >= $2
		  AND start_at < $3
		  AND withdrawn_at IS NULL
		ORDER BY start_at ASC
	`, lecturerID, from, to)
	if err != nil {
		return nil, fmt.Errorf("mengambil slot: %w", err)
	}
	defer rows.Close()

	var slots []Slot
	for rows.Next() {
		var slot Slot
		err := rows.Scan(&slot.ID, &slot.StartAt, &slot.EndAt, &slot.Status)
		if err != nil {
			return nil, fmt.Errorf("membaca baris slot: %w", err)
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi slot: %w", err)
	}

	return slots, nil
}
