// Package masterdata 維護基本資料：站點與線別、產品（含圖片）、BOM、途程、不良代碼。
package masterdata

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"mes/internal/auth"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// Register：public 不需登入（圖片供 <img> 直接載入）；scoped 已套用工廠範圍與權限。
func (h *Handler) Register(public, scoped *gin.RouterGroup) {
	public.GET("/images/:id", h.image)

	scoped.GET("/stations", auth.Require("station:view"), h.listStations)
	scoped.POST("/stations", auth.Require("station:edit"), h.createStation)
	scoped.PUT("/stations/:id", auth.Require("station:edit"), h.updateStation)
	scoped.POST("/lines", auth.Require("station:edit"), h.createLine)
	scoped.PUT("/lines/:id", auth.Require("station:edit"), h.updateLine)

	scoped.GET("/defect-codes", auth.Require("station:view"), h.listDefectCodes)
	scoped.POST("/defect-codes", auth.Require("defect:edit"), h.createDefectCode)
	scoped.PUT("/defect-codes/:id", auth.Require("defect:edit"), h.updateDefectCode)

	scoped.GET("/products", auth.Require("product:view"), h.listProducts)
	scoped.GET("/products/:id", auth.Require("product:view"), h.getProduct)
	scoped.POST("/products", auth.Require("product:edit"), h.createProduct)
	scoped.PUT("/products/:id", auth.Require("product:edit"), h.updateProduct)
	scoped.POST("/products/:id/images", auth.Require("product:edit"), h.uploadImage)
	scoped.PUT("/product-images/:id/primary", auth.Require("product:edit"), h.setPrimaryImage)
	scoped.DELETE("/product-images/:id", auth.Require("product:edit"), h.deleteImage)

	scoped.GET("/products/:id/bom", auth.Require("bom:view"), h.listBOM)
	scoped.POST("/bom", auth.Require("bom:edit"), h.createBOM)
	scoped.PUT("/bom/:id", auth.Require("bom:edit"), h.updateBOM)
	scoped.DELETE("/bom/:id", auth.Require("bom:edit"), h.deleteBOM)
	scoped.POST("/bom/:id/substitutes", auth.Require("bom:edit"), h.addSubstitute)
	scoped.DELETE("/bom-substitutes/:id", auth.Require("bom:edit"), h.deleteSubstitute)

	scoped.GET("/products/:id/routings", auth.Require("routing:view"), h.listRoutings)
	scoped.POST("/products/:id/routings", auth.Require("routing:edit"), h.createRouting)
	scoped.POST("/routings/:id/activate", auth.Require("routing:edit"), h.activateRouting)
}

func active(v *bool) bool {
	return v == nil || *v
}
