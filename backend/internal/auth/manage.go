package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"mes/internal/httpx"
)

// 角色與使用者維護。防止越權：非總部管理員只能授予自己本身具備的權限，
// 也只能指派權限不超過自己的角色；系統角色（最高權限）不可修改或刪除。

func (h *Handler) RegisterManage(scoped *gin.RouterGroup) {
	scoped.POST("/roles", Require("role:edit"), h.createRole)
	scoped.PUT("/roles/:id", Require("role:edit"), h.updateRole)
	scoped.DELETE("/roles/:id", Require("role:edit"), h.deleteRole)
	scoped.POST("/users", Require("user:edit"), h.createUser)
	scoped.PUT("/users/:id", Require("user:edit"), h.updateUser)
}

type roleInput struct {
	Code        string   `json:"code" binding:"max=50"`
	Name        string   `json:"name" binding:"required,max=100"`
	Permissions []string `json:"permissions"`
}

// checkGrant 確認權限碼有效，且目前使用者可以授予。
func checkGrant(c *gin.Context, codes []string) error {
	valid := map[string]bool{}
	for _, p := range Catalog {
		valid[p.Code] = true
	}
	for _, code := range codes {
		if !valid[code] {
			return httpx.Biz("權限碼不存在：%s", code)
		}
		if !Can(c, code) {
			return httpx.Biz("不能授予你自己沒有的權限：%s", code)
		}
	}
	return nil
}

func writeRolePerms(ctx context.Context, tx pgx.Tx, roleID uuid.UUID, codes []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_code) SELECT $1, unnest($2::text[]) ON CONFLICT DO NOTHING`,
		roleID, codes)
	return err
}

func audit(ctx context.Context, tx pgx.Tx, c *gin.Context, action, entity string, id uuid.UUID, after any) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (factory_id, user_id, action, entity, entity_id, after) VALUES ($1, $2, $3, $4, $5, $6)`,
		FactoryID(c), UserID(c), action, entity, id.String(), after)
	return err
}

func (h *Handler) createRole(c *gin.Context) {
	var in roleInput
	if !httpx.Bind(c, &in) {
		return
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if in.Code == "" {
		httpx.Fail(c, httpx.Biz("請輸入角色代碼"))
		return
	}
	if err := checkGrant(c, in.Permissions); err != nil {
		httpx.Fail(c, err)
		return
	}
	id := httpx.NewID()
	err := pgx.BeginFunc(c, h.store.pool, func(tx pgx.Tx) error {
		var dup bool
		if err := tx.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM roles WHERE factory_id = $1 AND code = $2)`,
			FactoryID(c), in.Code).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return httpx.Biz("角色代碼 %s 已存在", in.Code)
		}
		if _, err := tx.Exec(c, `INSERT INTO roles (id, factory_id, code, name) VALUES ($1, $2, $3, $4)`,
			id, FactoryID(c), in.Code, strings.TrimSpace(in.Name)); err != nil {
			return err
		}
		if err := writeRolePerms(c, tx, id, in.Permissions); err != nil {
			return err
		}
		return audit(c, tx, c, "CREATE", "role", id, in)
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// editableRole 鎖定本工廠的非系統角色。
func editableRole(c *gin.Context, tx pgx.Tx, id uuid.UUID) error {
	var system bool
	err := tx.QueryRow(c, `SELECT is_system FROM roles WHERE id = $1 AND factory_id = $2 FOR UPDATE`, id, FactoryID(c)).Scan(&system)
	if err == pgx.ErrNoRows {
		return httpx.Biz("角色不存在或為共用範本，不能在此修改")
	}
	if err != nil {
		return err
	}
	if system {
		return httpx.Biz("系統角色（最高權限）固定具備全部權限，不能修改或刪除")
	}
	return nil
}

func (h *Handler) updateRole(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in roleInput
	if !httpx.Bind(c, &in) {
		return
	}
	err := pgx.BeginFunc(c, h.store.pool, func(tx pgx.Tx) error {
		if err := editableRole(c, tx, id); err != nil {
			return err
		}
		// 只檢查「新增」的權限；移除自己沒有的權限是允許的
		rows, err := tx.Query(c, `SELECT permission_code FROM role_permissions WHERE role_id = $1`, id)
		if err != nil {
			return err
		}
		old, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		had := map[string]bool{}
		for _, p := range old {
			had[p] = true
		}
		added := []string{}
		keep := false
		for _, p := range in.Permissions {
			if !had[p] {
				added = append(added, p)
			}
			if p == "role:edit" {
				keep = true
			}
		}
		if err := checkGrant(c, added); err != nil {
			return err
		}
		// 避免把自己鎖在門外：拿掉自己角色的 role:edit 且沒有其他角色提供時拒絕
		if had["role:edit"] && !keep && !IsSuperAdmin(c) {
			var other bool
			if err := tx.QueryRow(c, `
				SELECT EXISTS (SELECT 1 FROM user_factory_roles ufr JOIN role_permissions rp ON rp.role_id = ufr.role_id
				               WHERE ufr.user_id = $1 AND ufr.factory_id = $2 AND ufr.role_id <> $3 AND rp.permission_code = 'role:edit')
				OR NOT EXISTS (SELECT 1 FROM user_factory_roles WHERE user_id = $1 AND factory_id = $2 AND role_id = $3)`,
				UserID(c), FactoryID(c), id).Scan(&other); err != nil {
				return err
			}
			if !other {
				return httpx.Biz("不能移除你自己唯一的「維護角色與權限」，否則將無法再修改角色")
			}
		}
		if _, err := tx.Exec(c, `UPDATE roles SET name = $2 WHERE id = $1`, id, strings.TrimSpace(in.Name)); err != nil {
			return err
		}
		if err := writeRolePerms(c, tx, id, in.Permissions); err != nil {
			return err
		}
		return audit(c, tx, c, "UPDATE", "role", id, in)
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) deleteRole(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	err := pgx.BeginFunc(c, h.store.pool, func(tx pgx.Tx) error {
		if err := editableRole(c, tx, id); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(c, `SELECT count(*) FROM user_factory_roles WHERE role_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return httpx.Biz("還有 %d 位使用者是此角色，請先改派其他角色再刪除", n)
		}
		if _, err := tx.Exec(c, `DELETE FROM roles WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(c, tx, c, "DELETE", "role", id, nil)
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type userInput struct {
	Email      string      `json:"email" binding:"max=200"`
	Name       string      `json:"name" binding:"required,max=100"`
	EmployeeNo string      `json:"employeeNo" binding:"max=30"`
	Password   string      `json:"password" binding:"max=72"`
	IsActive   *bool       `json:"isActive"`
	RoleIDs    []uuid.UUID `json:"roleIds"`
}

// assignRoles 以 roleIDs 取代使用者在本工廠的角色；只能指派本工廠角色，且角色權限不得超過自己。
func assignRoles(c *gin.Context, tx pgx.Tx, userID uuid.UUID, roleIDs []uuid.UUID) error {
	if len(roleIDs) == 0 {
		return httpx.Biz("請至少指派一個角色")
	}
	rows, err := tx.Query(c, `
		SELECT r.id, r.name, COALESCE(array_agg(rp.permission_code) FILTER (WHERE rp.permission_code IS NOT NULL), '{}')
		FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.id = ANY ($1) AND r.factory_id = $2 GROUP BY r.id`, roleIDs, FactoryID(c))
	if err != nil {
		return err
	}
	type role struct {
		ID    uuid.UUID
		Name  string
		Perms []string
	}
	roles, err := pgx.CollectRows(rows, pgx.RowToStructByPos[role])
	if err != nil {
		return err
	}
	if len(roles) != len(roleIDs) {
		return httpx.Biz("部分角色不存在")
	}
	for _, r := range roles {
		for _, p := range r.Perms {
			if !Can(c, p) {
				return httpx.Biz("角色「%s」含有你沒有的權限（%s），不能指派", r.Name, p)
			}
		}
	}
	if _, err := tx.Exec(c, `DELETE FROM user_factory_roles WHERE user_id = $1 AND factory_id = $2`, userID, FactoryID(c)); err != nil {
		return err
	}
	_, err = tx.Exec(c, `
		INSERT INTO user_factory_roles (user_id, factory_id, role_id) SELECT $1, $2, unnest($3::uuid[])`,
		userID, FactoryID(c), roleIDs)
	return err
}

func (h *Handler) createUser(c *gin.Context) {
	var in userInput
	if !httpx.Bind(c, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(in.Email, "@") {
		httpx.Fail(c, httpx.Biz("請輸入正確的 Email（登入帳號）"))
		return
	}
	if len(in.Password) < 8 {
		httpx.Fail(c, httpx.Biz("密碼至少 8 碼"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	id := httpx.NewID()
	err = pgx.BeginFunc(c, h.store.pool, func(tx pgx.Tx) error {
		var dup bool
		if err := tx.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, in.Email).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return httpx.Biz("Email %s 已有帳號", in.Email)
		}
		if _, err := tx.Exec(c, `
			INSERT INTO users (id, email, name, password_hash, employee_no) VALUES ($1, $2, $3, $4, NULLIF($5, ''))`,
			id, in.Email, strings.TrimSpace(in.Name), string(hash), strings.TrimSpace(in.EmployeeNo)); err != nil {
			return err
		}
		if err := assignRoles(c, tx, id, in.RoleIDs); err != nil {
			return err
		}
		return audit(c, tx, c, "CREATE", "user", id, gin.H{"email": in.Email, "name": in.Name, "roleIds": in.RoleIDs})
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) updateUser(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in userInput
	if !httpx.Bind(c, &in) {
		return
	}
	if in.Password != "" && len(in.Password) < 8 {
		httpx.Fail(c, httpx.Biz("密碼至少 8 碼"))
		return
	}
	err := pgx.BeginFunc(c, h.store.pool, func(tx pgx.Tx) error {
		var superAdmin, member bool
		err := tx.QueryRow(c, `
			SELECT u.is_super_admin, EXISTS (SELECT 1 FROM user_factory_roles WHERE user_id = u.id AND factory_id = $2)
			FROM users u WHERE u.id = $1 FOR UPDATE`, id, FactoryID(c)).Scan(&superAdmin, &member)
		if err == pgx.ErrNoRows || (err == nil && !member && !superAdmin) {
			return httpx.Biz("此使用者不屬於本工廠")
		}
		if err != nil {
			return err
		}
		if superAdmin {
			return httpx.Biz("總部管理員帳號不能在此修改")
		}
		self := id == UserID(c)
		if self && in.IsActive != nil && !*in.IsActive {
			return httpx.Biz("不能停用自己的帳號")
		}
		if _, err := tx.Exec(c, `
			UPDATE users SET name = $2, employee_no = NULLIF($3, ''), is_active = COALESCE($4, is_active), updated_at = now()
			WHERE id = $1`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.EmployeeNo), in.IsActive); err != nil {
			return err
		}
		if in.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(c, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, string(hash)); err != nil {
				return err
			}
		}
		// 自己的角色不在此變更，避免誤把自己的權限拿掉
		if !self {
			if err := assignRoles(c, tx, id, in.RoleIDs); err != nil {
				return err
			}
		}
		return audit(c, tx, c, "UPDATE", "user", id, gin.H{"name": in.Name, "isActive": in.IsActive,
			"roleIds": in.RoleIDs, "passwordReset": in.Password != ""})
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
