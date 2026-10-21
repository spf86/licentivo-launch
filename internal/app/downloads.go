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
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

const MaxProductUploadSize int64 = 1024 << 20

var productPlatforms = map[string]map[string]bool{
	"windows": {".exe": true, ".msi": true, ".msix": true, ".zip": true},
	"macos":   {".dmg": true, ".pkg": true, ".zip": true},
	"ios":     {".ipa": true},
	"android": {".apk": true, ".aab": true, ".xapk": true},
}

type ProductDownloadView struct {
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	Source      string `json:"source"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	FileName    string `json:"file_name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   uint64 `json:"size_bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type ProductDownloadInput struct {
	Source    string `json:"source"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	SizeBytes uint64 `json:"size_bytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

type ProductDownloadFile struct {
	File        *os.File
	Name        string
	ContentType string
	UpdatedAt   time.Time
}

func validateProductPlatform(platform string) error {
	if _, ok := productPlatforms[platform]; !ok {
		return problem(400, "PLATFORM_INVALID", "请选择 Windows、macOS、iOS 或 Android")
	}
	return nil
}

func validateProductVersion(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 80 {
		return "", problem(400, "DOWNLOAD_VERSION_INVALID", "请填写该平台的软件版本")
	}
	return version, nil
}

func validateUploadName(platform, name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return "", "", problem(400, "UPLOAD_NAME_INVALID", "安装包文件名无效")
	}
	for _, value := range name {
		if unicode.IsControl(value) {
			return "", "", problem(400, "UPLOAD_NAME_INVALID", "安装包文件名无效")
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !productPlatforms[platform][ext] {
		return "", "", problem(400, "UPLOAD_TYPE_INVALID", "安装包格式与所选平台不匹配")
	}
	return name, ext, nil
}

func (s *Service) productDownloadViews(ctx context.Context, productID string) ([]ProductDownloadView, error) {
	var rows []storage.ProductDownload
	if err := s.db.WithContext(ctx).Where("product_id = ?", productID).Order("FIELD(platform,'windows','macos','ios','android'),created_at").Find(&rows).Error; err != nil {
		return nil, ErrStorage
	}
	views := make([]ProductDownloadView, 0, len(rows))
	for _, row := range rows {
		views = append(views, productDownloadView(row))
	}
	return views, nil
}

func productDownloadView(row storage.ProductDownload) ProductDownloadView {
	view := ProductDownloadView{ID: row.ID, Platform: row.Platform, Source: row.Source, Version: row.Version, FileName: row.FileName, ContentType: row.ContentType, SizeBytes: row.SizeBytes}
	if len(row.SHA256) == sha256.Size {
		view.SHA256 = hex.EncodeToString(row.SHA256)
	}
	if row.Source == "external" && row.ExternalURL != nil {
		view.URL = *row.ExternalURL
	} else if row.Source == "upload" {
		view.URL = "/api/v1/downloads/" + url.PathEscape(row.ID) + "/" + url.PathEscape(row.FileName)
	}
	return view
}

func validateDownloadSHA256(value string) ([]byte, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, problem(400, "DOWNLOAD_CHECKSUM_INVALID", "SHA-256 校验值必须是 64 位十六进制字符")
	}
	return decoded, nil
}

func (s *Service) attachProductDownloads(ctx context.Context, view *ProductView) error {
	downloads, err := s.productDownloadViews(ctx, view.ID)
	if err != nil {
		return err
	}
	if len(downloads) == 0 && view.Content.DownloadURL != "" {
		downloads = append(downloads, ProductDownloadView{Platform: "universal", Source: "external", Version: view.Content.Version, URL: view.Content.DownloadURL})
	}
	view.Downloads = downloads
	return nil
}

func (s *Service) SaveProductDownload(ctx context.Context, p Principal, productID, platform string, in ProductDownloadInput) (ProductDownloadView, error) {
	if !p.CanManage() {
		return ProductDownloadView{}, ErrForbidden
	}
	if err := validateProductPlatform(platform); err != nil {
		return ProductDownloadView{}, err
	}
	version, err := validateProductVersion(in.Version)
	if err != nil {
		return ProductDownloadView{}, err
	}
	checksum, err := validateDownloadSHA256(in.SHA256)
	if err != nil {
		return ProductDownloadView{}, err
	}
	var oldStorage *string
	var saved storage.ProductDownload
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var product storage.Product
		if err := tx.First(&product, "id = ?", productID).Error; err != nil {
			return err
		}
		var row storage.ProductDownload
		found := true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "product_id = ? AND platform = ?", productID, platform).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			found = false
		}
		if in.Source == "retain" {
			if !found || row.Source != "upload" {
				return problem(409, "UPLOAD_REQUIRED", "请为该平台选择安装包")
			}
			if err := tx.Model(&row).Updates(map[string]any{"version": version, "updated_at": now()}).Error; err != nil {
				return err
			}
			saved = row
			saved.Version = version
			return audit(tx, p.User.ID, "product.download_update", "product_download", row.ID, platform)
		}
		if in.Source != "external" {
			return problem(400, "DOWNLOAD_SOURCE_INVALID", "请选择上传安装包或外部下载地址")
		}
		external := strings.TrimSpace(in.URL)
		parsed, parseErr := url.Parse(external)
		if len(external) > 2048 || parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return problem(400, "DOWNLOAD_URL_INVALID", "外部下载地址必须是 HTTPS 地址")
		}
		if !found {
			id, idErr := newID()
			if idErr != nil {
				return idErr
			}
			row = storage.ProductDownload{ID: id, ProductID: productID, Platform: platform, Source: "external", Version: version, ExternalURL: &external, SizeBytes: in.SizeBytes, SHA256: checksum}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			oldStorage = row.StorageKey
			if err := tx.Model(&row).Updates(map[string]any{"source": "external", "version": version, "external_url": external, "storage_key": nil, "file_name": "", "content_type": "", "size_bytes": in.SizeBytes, "sha256": checksum, "updated_at": now()}).Error; err != nil {
				return err
			}
			row.Source, row.Version, row.ExternalURL, row.StorageKey = "external", version, &external, nil
			row.FileName, row.ContentType, row.SizeBytes, row.SHA256 = "", "", in.SizeBytes, checksum
		}
		saved = row
		return audit(tx, p.User.ID, "product.download_external", "product_download", row.ID, platform)
	})
	if err != nil {
		return ProductDownloadView{}, databaseError(err)
	}
	if oldStorage != nil {
		s.removeProductFile(productID, *oldStorage)
	}
	return productDownloadView(saved), nil
}

func (s *Service) SaveUploadedProductDownload(ctx context.Context, p Principal, productID, platform, version, fileName string, source io.Reader) (ProductDownloadView, error) {
	if !p.CanManage() {
		return ProductDownloadView{}, ErrForbidden
	}
	if err := validateProductPlatform(platform); err != nil {
		return ProductDownloadView{}, err
	}
	version, err := validateProductVersion(version)
	if err != nil {
		return ProductDownloadView{}, err
	}
	fileName, ext, err := validateUploadName(platform, fileName)
	if err != nil {
		return ProductDownloadView{}, err
	}
	if s.uploadDir == "" {
		return ProductDownloadView{}, problem(503, "UPLOAD_STORAGE_UNAVAILABLE", "服务器尚未配置安装包存储目录")
	}
	var product storage.Product
	if err := s.db.WithContext(ctx).First(&product, "id = ?", productID).Error; err != nil {
		return ProductDownloadView{}, databaseError(err)
	}
	if err := os.MkdirAll(s.uploadDir, 0700); err != nil {
		return ProductDownloadView{}, ErrStorage
	}
	temporary, err := os.CreateTemp(s.uploadDir, ".incoming-*")
	if err != nil {
		return ProductDownloadView{}, ErrStorage
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(source, MaxProductUploadSize+1))
	if syncErr := temporary.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := temporary.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return ProductDownloadView{}, ErrStorage
	}
	if written == 0 || written > MaxProductUploadSize {
		return ProductDownloadView{}, problem(413, "UPLOAD_SIZE_INVALID", "安装包不能为空且不能超过 1 GB")
	}
	probe, err := os.Open(temporaryPath)
	if err != nil {
		return ProductDownloadView{}, ErrStorage
	}
	buffer := make([]byte, 512)
	read, _ := probe.Read(buffer)
	probe.Close()
	contentType := http.DetectContentType(buffer[:read])
	storageKey, err := newID()
	if err != nil {
		return ProductDownloadView{}, err
	}
	storageKey += ext
	productDir := filepath.Join(s.uploadDir, productID)
	if err := os.MkdirAll(productDir, 0700); err != nil {
		return ProductDownloadView{}, ErrStorage
	}
	finalPath := filepath.Join(productDir, storageKey)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return ProductDownloadView{}, ErrStorage
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(finalPath)
		}
	}()
	var oldStorage *string
	var saved storage.ProductDownload
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedProduct storage.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedProduct, "id = ?", productID).Error; err != nil {
			return err
		}
		var row storage.ProductDownload
		found := true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "product_id = ? AND platform = ?", productID, platform).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			found = false
		}
		if !found {
			id, idErr := newID()
			if idErr != nil {
				return idErr
			}
			row = storage.ProductDownload{ID: id, ProductID: productID, Platform: platform}
		} else {
			oldStorage = row.StorageKey
		}
		row.Source, row.Version, row.ExternalURL = "upload", version, nil
		row.StorageKey, row.FileName, row.ContentType = &storageKey, fileName, contentType
		row.SizeBytes, row.SHA256, row.UpdatedAt = uint64(written), hash.Sum(nil), now()
		if found {
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&row).Error; err != nil {
			return err
		}
		saved = row
		return audit(tx, p.User.ID, "product.download_upload", "product_download", row.ID, platform)
	})
	if err != nil {
		return ProductDownloadView{}, databaseError(err)
	}
	committed = true
	if oldStorage != nil && *oldStorage != storageKey {
		s.removeProductFile(productID, *oldStorage)
	}
	return productDownloadView(saved), nil
}

func (s *Service) DeleteProductDownload(ctx context.Context, p Principal, productID, platform string) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	if err := validateProductPlatform(platform); err != nil {
		return err
	}
	var storageKey *string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row storage.ProductDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "product_id = ? AND platform = ?", productID, platform).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		storageKey = row.StorageKey
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "product.download_delete", "product_download", row.ID, platform)
	})
	if err != nil {
		return databaseError(err)
	}
	if storageKey != nil {
		s.removeProductFile(productID, *storageKey)
	}
	return nil
}

func (s *Service) removeProductFile(productID, storageKey string) {
	if s.uploadDir == "" || filepath.Base(storageKey) != storageKey {
		return
	}
	_ = os.Remove(filepath.Join(s.uploadDir, productID, storageKey))
}

func (s *Service) OpenProductDownload(ctx context.Context, id string) (ProductDownloadFile, error) {
	var row storage.ProductDownload
	err := s.db.WithContext(ctx).
		Joins("JOIN products ON products.id = product_downloads.product_id").
		Where("product_downloads.id = ? AND product_downloads.source = 'upload' AND products.status = 'published'", id).
		First(&row).Error
	if err != nil || row.StorageKey == nil || s.uploadDir == "" || filepath.Base(*row.StorageKey) != *row.StorageKey {
		return ProductDownloadFile{}, ErrNotFound
	}
	file, err := os.Open(filepath.Join(s.uploadDir, row.ProductID, *row.StorageKey))
	if err != nil {
		return ProductDownloadFile{}, ErrNotFound
	}
	return ProductDownloadFile{File: file, Name: row.FileName, ContentType: row.ContentType, UpdatedAt: row.UpdatedAt}, nil
}
