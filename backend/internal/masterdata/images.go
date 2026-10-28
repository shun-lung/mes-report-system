package masterdata

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

const maxImageSize = 2 << 20 // 2 MB

var allowedImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}

// image 輸出圖片。不需登入：id 為不可預測的 UUID，且 <img> 無法帶 Authorization 標頭。
func (h *Handler) image(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var mime string
	var data []byte
	err := h.pool.QueryRow(c, `SELECT mime_type, data FROM product_images WHERE id = $1 AND data IS NOT NULL`, id).
		Scan(&mime, &data)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable") // 內容不會變，換圖即換 id
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, mime, data)
}

func (h *Handler) uploadImage(c *gin.Context) {
	productID, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	if _, ok := h.productInFactory(c, productID); !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImageSize+64<<10) // 含表單欄位的餘裕
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Abort(c, http.StatusBadRequest, "請選擇圖片檔")
		return
	}
	if fh.Size > maxImageSize {
		httpx.Fail(c, httpx.Biz("圖片不可超過 2 MB"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		httpx.Abort(c, http.StatusBadRequest, "讀取檔案失敗")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageSize+1))
	if err != nil || len(data) > maxImageSize {
		httpx.Fail(c, httpx.Biz("圖片不可超過 2 MB"))
		return
	}
	// 依檔案內容判斷格式，不信任副檔名
	mime := http.DetectContentType(data)
	if !allowedImageTypes[mime] {
		httpx.Fail(c, httpx.Biz("只接受 JPG、PNG、WebP、GIF 圖片"))
		return
	}

	id := httpx.NewID()
	_, err = h.pool.Exec(c, `
		INSERT INTO product_images (id, product_id, file_name, mime_type, file_size, data, sort_order, is_primary)
		SELECT $1, $2, $3, $4, $5, $6,
		       COALESCE((SELECT max(sort_order) + 1 FROM product_images WHERE product_id = $2), 0),
		       NOT EXISTS (SELECT 1 FROM product_images WHERE product_id = $2)`,
		id, productID, fh.Filename, mime, len(data), data)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// imageProduct 取得圖片所屬產品並確認屬於本工廠。
func (h *Handler) imageProduct(c *gin.Context, imageID uuid.UUID) (uuid.UUID, bool) {
	var productID uuid.UUID
	err := h.pool.QueryRow(c, `
		SELECT i.product_id FROM product_images i JOIN products p ON p.id = i.product_id
		WHERE i.id = $1 AND p.factory_id = $2`, imageID, auth.FactoryID(c)).Scan(&productID)
	if err != nil {
		httpx.DBError(c, err)
		return uuid.Nil, false
	}
	return productID, true
}

func (h *Handler) setPrimaryImage(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	productID, ok := h.imageProduct(c, id)
	if !ok {
		return
	}
	if _, err := h.pool.Exec(c, `UPDATE product_images SET is_primary = (id = $2) WHERE product_id = $1`, productID, id); err != nil {
		httpx.DBError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) deleteImage(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	productID, ok := h.imageProduct(c, id)
	if !ok {
		return
	}
	err := pgx.BeginFunc(c, h.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(c, `DELETE FROM product_images WHERE id = $1`, id); err != nil {
			return err
		}
		// 刪掉主圖時，由排序最前的圖片遞補
		_, err := tx.Exec(c, `
			UPDATE product_images SET is_primary = true
			WHERE id = (SELECT id FROM product_images WHERE product_id = $1 ORDER BY sort_order, created_at LIMIT 1)
			  AND NOT EXISTS (SELECT 1 FROM product_images WHERE product_id = $1 AND is_primary)`, productID)
		return err
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
