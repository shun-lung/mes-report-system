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
}

func (h *Handler) listUsers(c *gin.Context) {
	rows, err := h.store.pool.Query(c, `
		SELECT u.id, u.email, u.name, u.employee_no, u.is_super_admin, u.is_active,
		       COALESCE(array_agg(r.name ORDER BY r.code) FILTER (WHERE r.id IS NOT NULL), '{}')
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
