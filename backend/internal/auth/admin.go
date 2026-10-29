package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/httpx"
)

func (h *Handler) RegisterAdmin(scoped *gin.RouterGroup) {
	scoped.GET("/roles", Require("role:view"), h.listRoles)
	scoped.GET("/users", Require("user:view"), h.listUsers)
	scoped.GET("/users/:id/lines", Require("user:view"), h.userLines)
	scoped.PUT("/users/:id/lines", Require("user:edit"), h.setUserLines)
}

type roleRow struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	UserCount   int       `json:"userCount"`
}

func (h *Handler) listRoles(c *gin.Context) {
	rows, err := h.store.pool.Query(c, `
		SELECT r.id, r.code, r.name,
		       COALESCE((SELECT array_agg(rp.permission_code ORDER BY p.sort_order)
		                 FROM role_permissions rp JOIN permissions p ON p.code = rp.permission_code
		                 WHERE rp.role_id = r.id), '{}'),
		       (SELECT count(*) FROM user_factory_roles ufr WHERE ufr.role_id = r.id AND ufr.factory_id = $1)::int
		FROM roles r
		WHERE r.factory_id = $1 OR r.factory_id IS NULL
		ORDER BY r.code`, FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[roleRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type userRow struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	EmployeeNo   *string   `json:"employeeNo"`
	IsSuperAdmin bool      `json:"isSuperAdmin"`
	IsActive     bool      `json:"isActive"`
	Roles        []string  `json:"roles"`
	Lines        []string  `json:"lines"` // 授權線別代碼
}

func (h *Handler) listUsers(c *gin.Context) {
	rows, err := h.store.pool.Query(c, `
		SELECT u.id, u.email, u.name, u.employee_no, u.is_super_admin, u.is_active,
		       COALESCE(array_agg(r.name ORDER BY r.code) FILTER (WHERE r.id IS NOT NULL), '{}'),
		       ARRAY(SELECT l.code FROM user_line_assignments a JOIN production_lines l ON l.id = a.line_id
		             WHERE a.user_id = u.id AND a.factory_id = $1 ORDER BY l.code)
		FROM users u
		LEFT JOIN user_factory_roles ufr ON ufr.user_id = u.id AND ufr.factory_id = $1
		LEFT JOIN roles r ON r.id = ufr.role_id
		WHERE u.is_super_admin OR ufr.user_id IS NOT NULL
		GROUP BY u.id
		ORDER BY u.is_super_admin DESC, u.email`, FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[userRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) userLines(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	rows, err := h.store.pool.Query(c, `SELECT line_id FROM user_line_assignments WHERE user_id = $1 AND factory_id = $2`, id, FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, ids)
}

type userLinesInput struct {
	LineIDs []uuid.UUID `json:"lineIds"`
}

// setUserLines 設定使用者在本工廠可回報的線別（整批取代）。
func (h *Handler) setUserLines(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in userLinesInput
	if !httpx.Bind(c, &in) {
		return
	}
	factoryID := FactoryID(c)
	err := pgx.BeginFunc(c, h.store.pool, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM user_factory_roles WHERE user_id = $1 AND factory_id = $2)`,
			id, factoryID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return httpx.Biz("此使用者不屬於本工廠")
		}
		if _, err := tx.Exec(c, `DELETE FROM user_line_assignments WHERE user_id = $1 AND factory_id = $2`, id, factoryID); err != nil {
			return err
		}
		tag, err := tx.Exec(c, `
			INSERT INTO user_line_assignments (user_id, factory_id, line_id)
			SELECT $1, $2, l.id FROM production_lines l WHERE l.factory_id = $2 AND l.id = ANY ($3)`, id, factoryID, in.LineIDs)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(in.LineIDs) {
			return httpx.Biz("部分線別不存在")
		}
		return nil
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
