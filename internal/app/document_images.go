package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"licentivo-launch/internal/storage"
)

const MaxProductDocumentImageSize int64 = 8 << 20

var documentImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

type ProductDocumentImageView struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   uint64 `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

type ProductDocumentImageFile struct {
	File        *os.File
	Name        string
	ContentType string
	UpdatedAt   time.Time
}

func validateDocumentImageName(name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", "", problem(400, "DOCUMENT_IMAGE_NAME_INVALID", "图片文件名无效")
	}
	for _, value := range name {
		if unicode.IsControl(value) {
			return "", "", problem(400, "DOCUMENT_IMAGE_NAME_INVALID", "图片文件名无效")
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := documentImageTypes[ext]; !ok {
		return "", "", problem(400, "DOCUMENT_IMAGE_TYPE_INVALID", "文档图片只支持 PNG、JPEG、WebP 或 GIF")
	}
	return name, ext, nil
}

func productDocumentImageView(row storage.ProductDocumentImage) ProductDocumentImageView {
	view := ProductDocumentImageView{
		ID:          row.ID,
		URL:         "/api/v1/product-document-images/" + url.PathEscape(row.ID),
		FileName:    row.FileName,
		ContentType: row.ContentType,
		SizeBytes:   row.SizeBytes,
	}
	if len(row.SHA256) == sha256.Size {
		view.SHA256 = hex.EncodeToString(row.SHA256)
	}
	return view
}

func (s *Service) SaveProductDocumentImage(ctx context.Context, p Principal, productID, fileName string, source io.Reader) (ProductDocumentImageView, error) {
	if !p.CanManage() {
		return ProductDocumentImageView{}, ErrForbidden
	}
	fileName, ext, err := validateDocumentImageName(fileName)
	if err != nil {
		return ProductDocumentImageView{}, err
	}
	if s.uploadDir == "" {
		return ProductDocumentImageView{}, problem(503, "UPLOAD_STORAGE_UNAVAILABLE", "服务器尚未配置上传存储目录")
	}
	var product storage.Product
	if err := s.db.WithContext(ctx).First(&product, "id = ?", productID).Error; err != nil {
		return ProductDocumentImageView{}, databaseError(err)
	}
	if err := os.MkdirAll(s.uploadDir, 0700); err != nil {
		return ProductDocumentImageView{}, ErrStorage
	}
	temporary, err := os.CreateTemp(s.uploadDir, ".document-image-*")
	if err != nil {
		return ProductDocumentImageView{}, ErrStorage
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(source, MaxProductDocumentImageSize+1))
	if syncErr := temporary.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := temporary.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return ProductDocumentImageView{}, ErrStorage
	}
	if written == 0 || written > MaxProductDocumentImageSize {
		return ProductDocumentImageView{}, problem(413, "DOCUMENT_IMAGE_SIZE_INVALID", "文档图片不能为空且不能超过 8 MB")
	}
	probe, err := os.Open(temporaryPath)
	if err != nil {
		return ProductDocumentImageView{}, ErrStorage
	}
	buffer := make([]byte, 512)
	read, _ := probe.Read(buffer)
	_ = probe.Close()
	contentType := http.DetectContentType(buffer[:read])
	if contentType != documentImageTypes[ext] {
		return ProductDocumentImageView{}, problem(400, "DOCUMENT_IMAGE_CONTENT_INVALID", "图片内容与文件扩展名不一致")
	}
	id, err := newID()
	if err != nil {
		return ProductDocumentImageView{}, err
	}
	storageKey := id + ext
	productDir := filepath.Join(s.uploadDir, productID, "documents")
	if err := os.MkdirAll(productDir, 0700); err != nil {
		return ProductDocumentImageView{}, ErrStorage
	}
	finalPath := filepath.Join(productDir, storageKey)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return ProductDocumentImageView{}, ErrStorage
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(finalPath)
		}
	}()
	row := storage.ProductDocumentImage{
		ID:          id,
		ProductID:   productID,
		StorageKey:  storageKey,
		FileName:    fileName,
		ContentType: contentType,
		SizeBytes:   uint64(written),
		SHA256:      hash.Sum(nil),
		UpdatedAt:   now(),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedProduct storage.Product
		if err := tx.First(&lockedProduct, "id = ?", productID).Error; err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "product.document_image_upload", "product_document_image", row.ID, fileName)
	})
	if err != nil {
		return ProductDocumentImageView{}, databaseError(err)
	}
	committed = true
	return productDocumentImageView(row), nil
}

func (s *Service) OpenProductDocumentImage(ctx context.Context, id string) (ProductDocumentImageFile, error) {
	var row storage.ProductDocumentImage
	err := s.db.WithContext(ctx).
		Joins("JOIN products ON products.id = product_document_images.product_id").
		Where("product_document_images.id = ? AND products.status = 'published'", id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err != nil || s.uploadDir == "" || filepath.Base(row.StorageKey) != row.StorageKey {
		return ProductDocumentImageFile{}, ErrNotFound
	}
	file, err := os.Open(filepath.Join(s.uploadDir, row.ProductID, "documents", row.StorageKey))
	if err != nil {
		return ProductDocumentImageFile{}, ErrNotFound
	}
	return ProductDocumentImageFile{File: file, Name: row.FileName, ContentType: row.ContentType, UpdatedAt: row.UpdatedAt}, nil
}
