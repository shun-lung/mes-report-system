package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	EmployeeNo   *string   `json:"employeeNo"`
	IsSuperAdmin bool      `json:"isSuperAdmin"`
	passwordHash string
	isActive     bool
}

type FactoryAccess struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Timezone    string    `json:"timezone"`
	Permissions []string  `json:"permissions"`
}

func (s *Store) userByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, employee_no, is_super_admin, password_hash, is_active
		FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.EmployeeNo, &u.IsSuperAdmin, &u.passwordHash, &u.isActive)
	return u, err
}

func (s *Store) userByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, employee_no, is_super_admin, password_hash, is_active
		FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.EmployeeNo, &u.IsSuperAdmin, &u.passwordHash, &u.isActive)
	return u, err
}

func (s *Store) touchLogin(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return err
}

// FactoryPermissions 回傳使用者在該工廠的權限；member=false 表示無權進入此工廠。
func (s *Store) FactoryPermissions(ctx context.Context, userID, factoryID uuid.UUID, superAdmin bool) (perms map[string]bool, member bool, err error) {
	if superAdmin {
		var exists bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM factories WHERE id = $1)`, factoryID).Scan(&exists)
		if err != nil || !exists {
			return nil, false, err
		}
		return allPermissions(), true, nil
	}

	err = s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_factory_roles ufr
			JOIN users u ON u.id = ufr.user_id
			JOIN factories f ON f.id = ufr.factory_id
			WHERE ufr.user_id = $1 AND ufr.factory_id = $2 AND u.is_active AND f.is_active)`,
		userID, factoryID).Scan(&member)
	if err != nil || !member {
		return nil, false, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT rp.permission_code
		FROM user_factory_roles ufr
		JOIN role_permissions rp ON rp.role_id = ufr.role_id
		WHERE ufr.user_id = $1 AND ufr.factory_id = $2`, userID, factoryID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	perms = map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, false, err
		}
		perms[code] = true
	}
	return perms, true, rows.Err()
}

// Factories 列出使用者可進入的工廠與各廠權限。
func (s *Store) Factories(ctx context.Context, userID uuid.UUID, superAdmin bool) ([]FactoryAccess, error) {
	if superAdmin {
		all := make([]string, len(Catalog))
		for i, p := range Catalog {
			all[i] = p.Code
		}
		rows, err := s.pool.Query(ctx, `SELECT id, code, name, timezone FROM factories WHERE is_active ORDER BY code`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		list := []FactoryAccess{}
		for rows.Next() {
			f := FactoryAccess{Permissions: all}
			if err := rows.Scan(&f.ID, &f.Code, &f.Name, &f.Timezone); err != nil {
				return nil, err
			}
			list = append(list, f)
		}
		return list, rows.Err()
	}

	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.code, f.name, f.timezone,
		       COALESCE(array_agg(DISTINCT rp.permission_code) FILTER (WHERE rp.permission_code IS NOT NULL), '{}')
		FROM user_factory_roles ufr
		JOIN factories f ON f.id = ufr.factory_id
		LEFT JOIN role_permissions rp ON rp.role_id = ufr.role_id
		WHERE ufr.user_id = $1 AND f.is_active
		GROUP BY f.id
		ORDER BY f.code`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []FactoryAccess{}
	for rows.Next() {
		var f FactoryAccess
		if err := rows.Scan(&f.ID, &f.Code, &f.Name, &f.Timezone, &f.Permissions); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}
