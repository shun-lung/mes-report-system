package production

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type customerRow struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	IsActive bool      `json:"isActive"`
	Orders   int       `json:"orders"`
}

type customerInput struct {
	Code     string `json:"code" binding:"required,max=20"`
	Name     string `json:"name" binding:"required,max=100"`
	IsActive *bool  `json:"isActive"`
}

func (h *Handler) listCustomers(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, `
		SELECT cu.id, cu.code, cu.name, cu.is_active,
		       (SELECT count(*) FROM sales_orders so WHERE so.customer_id = cu.id)::int
		FROM customers cu WHERE cu.factory_id = $1 ORDER BY cu.code`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[customerRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) saveCustomer(c *gin.Context) {
	var in customerInput
	if !httpx.Bind(c, &in) {
		return
	}
	active := in.IsActive == nil || *in.IsActive
	factoryID := auth.FactoryID(c)
	if c.Param("id") == "" {
		id := httpx.NewID()
		if _, err := h.svc.pool.Exec(c, `INSERT INTO customers (id, factory_id, code, name, is_active) VALUES ($1, $2, $3, $4, $5)`,
			id, factoryID, in.Code, in.Name, active); err != nil {
			httpx.DBError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": id})
		return
	}
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	tag, err := h.svc.pool.Exec(c, `UPDATE customers SET code = $3, name = $4, is_active = $5 WHERE id = $1 AND factory_id = $2`,
		id, factoryID, in.Code, in.Name, active)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.DBError(c, pgx.ErrNoRows)
		return
	}
	c.Status(http.StatusNoContent)
}
